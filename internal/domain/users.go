package domain

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/felixtrav/sharedcal/internal/auth"
	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

// UserRef is the public face of a user.
type UserRef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
}

func refOf(u *store.User) UserRef {
	if u == nil {
		return UserRef{}
	}
	name := u.DisplayName
	if name == "" {
		name = u.Email
	}
	return UserRef{ID: u.ID, Name: name, AvatarURL: u.AvatarURL}
}

// userRefs loads refs for a set of user IDs.
func userRefs(db *gorm.DB, ids []string) (map[string]UserRef, error) {
	out := map[string]UserRef{}
	ids = uniq(ids)
	if len(ids) == 0 {
		return out, nil
	}
	var users []store.User
	if err := db.Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	for i := range users {
		out[users[i].ID] = refOf(&users[i])
	}
	return out, nil
}

func uniq(ids []string) []string {
	seen := map[string]bool{}
	out := ids[:0:0]
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// Provision implements auth.Provisioner: look the user up by (issuer, sub) —
// never by email — create them on first login, refresh profile claims, and
// resolve their role.
func (s *Service) Provision(ctx context.Context, id auth.Identity) (*store.User, error) {
	if id.Issuer == "" || id.Subject == "" {
		return nil, auth.ErrLoginRefused
	}
	groupsJSON, _ := json.Marshal(s.Opts.Roles.Mapped(id.Groups))
	bootstrap := s.isBootstrap(id)
	mapped, _, allowed := s.Opts.Roles.Resolve(id.Groups)

	var out store.User
	err := s.write(ctx, authz.Actor{}, func(tx *Tx) error {
		var u store.User
		err := tx.db.Where("oidc_issuer = ? AND oidc_subject = ?", id.Issuer, id.Subject).Take(&u).Error
		isNew := isNotFound(err)
		if err != nil && !isNew {
			return err
		}
		if !isNew && !u.Active {
			return auth.ErrLoginRefused
		}
		before := u
		now := tx.now
		if isNew {
			if !allowed && !bootstrap {
				return auth.ErrLoginRefused
			}
			u = store.User{ID: store.NewID(), OIDCIssuer: id.Issuer, OIDCSubject: id.Subject, Active: true, RoleSource: store.RoleSourceIDP, CreatedAt: now}
			if def, ok := s.settingDefaultRole(tx.db); ok && !mappedByGroup(s.Opts.Roles, id.Groups) {
				mapped = def
			}
		}
		u.Email = truncateRunes(id.Email, 320)
		u.DisplayName = truncateRunes(id.Name, 200)
		u.AvatarURL = truncateRunes(id.Picture, 2048)
		u.GroupsJSON = string(groupsJSON)
		u.LastLoginAt = &now
		u.UpdatedAt = now

		switch {
		case bootstrap && (isNew || u.Role != string(authz.Admin)):
			// The bootstrap identity is pinned as admin so a later IdP
			// recompute can never lock a fresh install out.
			u.Role, u.RoleSource = string(authz.Admin), store.RoleSourceManual
		case u.RoleSource == store.RoleSourceManual && !isNew:
			// Pinned by an admin; keep it.
		default:
			if !allowed {
				return auth.ErrLoginRefused
			}
			if !isNew && u.Role == string(authz.Admin) && mapped != authz.Admin {
				last, err := isLastAdmin(tx.db, u.ID)
				if err != nil {
					return err
				}
				if last {
					s.Log.WarnContext(ctx, "keeping last active admin despite IdP groups", "user", u.ID)
					mapped = authz.Admin
				}
			}
			u.Role = string(mapped)
		}
		if isNew {
			if err := tx.db.Create(&u).Error; err != nil {
				return err
			}
			if err := tx.record("user.created", "user", u.ID, nil, nil, adminView(&u, 0)); err != nil {
				return err
			}
		} else if err := tx.db.Save(&u).Error; err != nil {
			return err
		}
		if !isNew && refOf(&before).Name != refOf(&u).Name {
			if err := tx.retitleForPerson(u.ID); err != nil {
				return err
			}
		}
		if !isNew && before.Role != u.Role {
			if err := tx.recordAs("", "user.role_changed", "user", u.ID, nil, map[string]string{"role": before.Role}, map[string]string{"role": u.Role, "source": "idp"}); err != nil {
				return err
			}
			if err := tx.emit("user.role_changed", u.ID, 0, map[string]any{"userId": u.ID, "role": u.Role, "active": u.Active}, ""); err != nil {
				return err
			}
		}
		out = u
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func mappedByGroup(m auth.RoleMapping, groups []string) bool {
	_, matched, _ := m.Resolve(groups)
	return matched
}

func (s *Service) isBootstrap(id auth.Identity) bool {
	if s.Opts.BootstrapSub != "" && id.Subject == s.Opts.BootstrapSub {
		return true
	}
	// Only a verified email may claim the bootstrap role.
	return s.Opts.BootstrapEmail != "" && id.EmailVerified && strings.EqualFold(id.Email, s.Opts.BootstrapEmail)
}

func isLastAdmin(db *gorm.DB, userID string) (bool, error) {
	var n int64
	err := db.Model(&store.User{}).Where("role = ? AND active = ? AND id <> ?", string(authz.Admin), true, userID).Count(&n).Error
	return n == 0, err
}

// Me returns the caller's user row and preferences.
func (s *Service) Me(ctx context.Context, actor authz.Actor) (*store.User, Preferences, error) {
	var u store.User
	if err := s.DB.R.WithContext(ctx).Where("id = ?", actor.ID).Take(&u).Error; err != nil {
		return nil, Preferences{}, err
	}
	p, err := s.preferences(s.DB.R.WithContext(ctx), actor.ID)
	return &u, p, err
}

// Directory lists active users for @mentions and the owner filter.
func (s *Service) Directory(ctx context.Context, actor authz.Actor) ([]UserRef, error) {
	if !authz.CanView(actor) {
		return nil, forbidden("not allowed")
	}
	var users []store.User
	if err := s.DB.R.WithContext(ctx).Where("active = ?", true).Order("display_name, id").Limit(5000).Find(&users).Error; err != nil {
		return nil, err
	}
	out := make([]UserRef, len(users))
	for i := range users {
		out[i] = refOf(&users[i])
	}
	return out, nil
}

// AdminUser is the People page row.
type AdminUser struct {
	UserRef
	Email        string     `json:"email"`
	Role         string     `json:"role" enum:"viewer,writer,admin"`
	RoleSource   string     `json:"roleSource" enum:"idp,manual"`
	Active       bool       `json:"active"`
	Groups       []string   `json:"groups"`
	LastLoginAt  *time.Time `json:"lastLoginAt"`
	SessionCount int64      `json:"sessionCount"`
	Erased       bool       `json:"erased"`
}

func adminView(u *store.User, sessions int64) AdminUser {
	var groups []string
	_ = json.Unmarshal([]byte(u.GroupsJSON), &groups)
	if groups == nil {
		groups = []string{}
	}
	return AdminUser{UserRef: refOf(u), Email: u.Email, Role: u.Role, RoleSource: u.RoleSource, Active: u.Active, Groups: groups, LastLoginAt: u.LastLoginAt, SessionCount: sessions, Erased: u.ErasedAt != nil}
}

// ListUsers is the admin People list.
func (s *Service) ListUsers(ctx context.Context, actor authz.Actor) ([]AdminUser, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	db := s.DB.R.WithContext(ctx)
	var users []store.User
	if err := db.Order("display_name, id").Find(&users).Error; err != nil {
		return nil, err
	}
	type cnt struct {
		UserID string
		N      int64
	}
	var counts []cnt
	if err := db.Model(&store.Session{}).Select("user_id, COUNT(*) AS n").Where("expires_at > ?", s.now()).Group("user_id").Scan(&counts).Error; err != nil {
		return nil, err
	}
	byUser := map[string]int64{}
	for _, c := range counts {
		byUser[c.UserID] = c.N
	}
	out := make([]AdminUser, len(users))
	for i := range users {
		out[i] = adminView(&users[i], byUser[users[i].ID])
	}
	return out, nil
}

// UserPatch changes a user's role, role source or active flag.
type UserPatch struct {
	Role       *string `json:"role,omitempty" enum:"viewer,writer,admin"`
	RoleSource *string `json:"roleSource,omitempty" enum:"idp,manual"`
	Active     *bool   `json:"active,omitempty"`
}

// UpdateUser applies an admin's change. Setting a role pins it (manual);
// resetting the source to idp recomputes from the stored groups. The last
// active admin can be neither demoted nor deactivated.
func (s *Service) UpdateUser(ctx context.Context, actor authz.Actor, id string, p UserPatch) (*AdminUser, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	var out AdminUser
	var revoke bool
	err := s.write(ctx, actor, func(tx *Tx) error {
		u, err := take[store.User](tx.db, id, "user")
		if err != nil {
			return err
		}
		before := adminView(&u, 0)
		if p.Role != nil {
			if !authz.Role(*p.Role).Valid() {
				return invalidField("role", "unknown role")
			}
			u.Role, u.RoleSource = *p.Role, store.RoleSourceManual
		}
		if p.RoleSource != nil {
			switch *p.RoleSource {
			case store.RoleSourceManual:
				u.RoleSource = store.RoleSourceManual
			case store.RoleSourceIDP:
				u.RoleSource = store.RoleSourceIDP
				var groups []string
				_ = json.Unmarshal([]byte(u.GroupsJSON), &groups)
				role, _, ok := s.Opts.Roles.Resolve(groups)
				if !ok {
					role = authz.Viewer
				}
				if p.Role == nil {
					u.Role = string(role)
				}
			default:
				return invalidField("roleSource", "must be idp or manual")
			}
		}
		if p.Active != nil {
			u.Active = *p.Active
		}
		if before.Role == string(authz.Admin) && before.Active && (u.Role != string(authz.Admin) || !u.Active) {
			last, err := isLastAdmin(tx.db, u.ID)
			if err != nil {
				return err
			}
			if last {
				return conflict("this is the last active admin; promote someone else first", nil)
			}
		}
		u.UpdatedAt = tx.now
		if err := tx.db.Save(&u).Error; err != nil {
			return err
		}
		out = adminView(&u, 0)
		if err := tx.record("user.updated", "user", u.ID, nil, before, out); err != nil {
			return err
		}
		revoke = before.Active && !u.Active
		if before.Role != u.Role || before.Active != u.Active {
			return tx.emit("user.role_changed", u.ID, 0, map[string]any{"userId": u.ID, "role": u.Role, "active": u.Active}, "")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if revoke && s.Sessions != nil {
		if err := s.Sessions.RevokeUser(ctx, id, ""); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

// SessionInfo describes one session for the People page. ID is a prefix of
// the stored hash: enough to target a revoke, useless as a credential.
type SessionInfo struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	IP         string    `json:"ip"`
	UserAgent  string    `json:"userAgent"`
	Current    bool      `json:"current"`
}

const sessionIDPrefix = 16

// ListSessions lists a user's live sessions.
func (s *Service) ListSessions(ctx context.Context, actor authz.Actor, userID, currentHash string) ([]SessionInfo, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	var rows []store.Session
	if err := s.DB.R.WithContext(ctx).Where("user_id = ? AND expires_at > ?", userID, s.now()).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]SessionInfo, len(rows))
	for i, r := range rows {
		out[i] = SessionInfo{ID: r.ID[:sessionIDPrefix], CreatedAt: r.CreatedAt, LastSeenAt: r.LastSeenAt, ExpiresAt: r.ExpiresAt, IP: r.IP, UserAgent: r.UserAgent, Current: r.ID == currentHash}
	}
	return out, nil
}

// RevokeSessions revokes one session (by ID prefix) or all of a user's.
func (s *Service) RevokeSessions(ctx context.Context, actor authz.Actor, userID, sessionID string) error {
	if !authz.CanAdminister(actor) {
		return forbidden("admins only")
	}
	if sessionID == "" {
		if err := s.Sessions.RevokeUser(ctx, userID, ""); err != nil {
			return err
		}
	} else {
		if len(sessionID) != sessionIDPrefix || strings.Trim(sessionID, "0123456789abcdef") != "" {
			return invalidField("id", "invalid session id")
		}
		var hashes []string
		if err := s.DB.R.WithContext(ctx).Model(&store.Session{}).Where("user_id = ? AND id LIKE ?", userID, sessionID+"%").Pluck("id", &hashes).Error; err != nil {
			return err
		}
		if len(hashes) != 1 {
			return notFound("session")
		}
		if err := s.Sessions.Revoke(ctx, hashes[0]); err != nil {
			return err
		}
	}
	return s.write(ctx, actor, func(tx *Tx) error {
		return tx.record("user.sessions_revoked", "user", userID, nil, nil, map[string]string{"session": sessionID})
	})
}

// TransferOwnership moves every event owned by one user to another.
func (s *Service) TransferOwnership(ctx context.Context, actor authz.Actor, fromID, toID string) (int64, error) {
	if !authz.CanTransferOwnership(actor) {
		return 0, forbidden("admins only")
	}
	if fromID == toID {
		return 0, invalidField("toUserId", "choose a different user")
	}
	var moved int64
	err := s.write(ctx, actor, func(tx *Tx) error {
		var err error
		moved, err = s.transferEvents(tx, fromID, toID)
		return err
	})
	return moved, err
}

// transferEvents moves fromID's live events to toID inside tx.
func (s *Service) transferEvents(tx *Tx, fromID, toID string) (int64, error) {
	var to store.User
	if err := tx.db.Where("id = ? AND active = ?", toID, true).Take(&to).Error; err != nil {
		if isNotFound(err) {
			return 0, invalidField("toUserId", "target user not found or inactive")
		}
		return 0, err
	}
	if to.Role == string(authz.Viewer) {
		return 0, invalidField("toUserId", "target user must be a writer or admin")
	}
	var ids []string
	if err := tx.db.Model(&store.Event{}).Where("owner_id = ? AND deleted_at IS NULL", fromID).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	res := tx.db.Model(&store.Event{}).Where("id IN ?", ids).
		Updates(map[string]any{"owner_id": toID, "version": gorm.Expr("version + 1"), "updated_at": tx.now})
	if res.Error != nil {
		return 0, res.Error
	}
	if err := tx.record("user.events_transferred", "user", fromID, nil, nil, map[string]any{"to": toID, "count": res.RowsAffected}); err != nil {
		return 0, err
	}
	for _, id := range ids {
		ev, err := s.loadEvent(tx.db, id, false)
		if err != nil {
			return 0, err
		}
		dto, err := s.eventDTOs(tx.db, tx.actor, []store.Event{*ev})
		if err != nil {
			return 0, err
		}
		if err := tx.emit("event.updated", id, ev.Version, dto[0], ""); err != nil {
			return 0, err
		}
	}
	return res.RowsAffected, nil
}

// Preferences are per-user display settings.
type Preferences struct {
	ThemeID     string `json:"themeId" maxLength:"64"`
	ColorMode   string `json:"colorMode" enum:"light,dark,system"`
	TimeZone    string `json:"timeZone" maxLength:"64"`
	WeekStart   int    `json:"weekStart" enum:"0,1,2,3,4,5,6"`
	Clock24h    bool   `json:"clock24h"`
	DefaultView string `json:"defaultView" enum:"month,week,day,agenda"`
	Density     string `json:"density" enum:"comfortable,compact"`
}

func defaultPreferences() Preferences {
	return Preferences{ThemeID: "default", ColorMode: "system", WeekStart: 1, DefaultView: "month", Density: "comfortable"}
}

func (s *Service) preferences(db *gorm.DB, userID string) (Preferences, error) {
	var row store.UserPreference
	err := db.Where("user_id = ?", userID).Take(&row).Error
	if isNotFound(err) {
		return defaultPreferences(), nil
	}
	if err != nil {
		return Preferences{}, err
	}
	return Preferences{ThemeID: row.ThemeID, ColorMode: row.ColorMode, TimeZone: row.TimeZone, WeekStart: row.WeekStart, Clock24h: row.Clock24h, DefaultView: row.DefaultView, Density: row.Density}, nil
}

// SavePreferences validates and stores the caller's preferences.
func (s *Service) SavePreferences(ctx context.Context, actor authz.Actor, p Preferences) (Preferences, error) {
	if !authz.CanView(actor) {
		return Preferences{}, forbidden("not allowed")
	}
	var fe fieldErrors
	if p.TimeZone != "" {
		if _, err := loadLocation(p.TimeZone); err != nil {
			fe.add("timeZone", "unknown time zone")
		}
	}
	if !oneOf(p.ColorMode, "light", "dark", "system") {
		fe.add("colorMode", "must be light, dark or system")
	}
	if !oneOf(p.DefaultView, "month", "week", "day", "agenda") {
		fe.add("defaultView", "unknown view")
	}
	if !oneOf(p.Density, "comfortable", "compact") {
		fe.add("density", "must be comfortable or compact")
	}
	if p.WeekStart < 0 || p.WeekStart > 6 {
		fe.add("weekStart", "must be 0–6")
	}
	if !validThemeID(p.ThemeID) {
		fe.add("themeId", "invalid theme id")
	}
	if err := fe.err(); err != nil {
		return Preferences{}, err
	}
	now := s.now()
	row := store.UserPreference{UserID: actor.ID, ThemeID: p.ThemeID, ColorMode: p.ColorMode, TimeZone: p.TimeZone, WeekStart: p.WeekStart, Clock24h: p.Clock24h, DefaultView: p.DefaultView, Density: p.Density, CreatedAt: now, UpdatedAt: now}
	err := s.write(ctx, actor, func(tx *Tx) error {
		var existing store.UserPreference
		err := tx.db.Where("user_id = ?", actor.ID).Take(&existing).Error
		if isNotFound(err) {
			return tx.db.Create(&row).Error
		}
		if err != nil {
			return err
		}
		row.CreatedAt = existing.CreatedAt
		return tx.db.Save(&row).Error
	})
	return p, err
}

func oneOf(v string, options ...string) bool {
	for _, o := range options {
		if v == o {
			return true
		}
	}
	return false
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ErasedName is the display name an erased user's row keeps, so audit
// history still reads sensibly.
const ErasedName = "Deleted user"

// EraseResult summarizes an erasure.
type EraseResult struct {
	EventsTransferred  int64 `json:"eventsTransferred"`
	CommentsRemoved    int   `json:"commentsRemoved"`
	AttachmentsRemoved int   `json:"attachmentsRemoved"`
	SuggestionsRemoved int   `json:"suggestionsRemoved"`
}

// EraseUser hard-deletes a user's personal data on request. Their live
// events must go to another writer (transferTo) first; their comments,
// uploads, suggestions, inbox, preferences, feed token and sessions are
// deleted; audit snapshots of that content are scrubbed. The user row stays,
// anonymized, so history keeps an actor ("Deleted user"); its OIDC subject is
// cleared, so signing in again creates a new account.
func (s *Service) EraseUser(ctx context.Context, actor authz.Actor, id, transferTo string) (*EraseResult, error) {
	if !authz.CanEraseUser(actor, id) {
		if authz.CanAdminister(actor) {
			return nil, invalid("you cannot erase your own account")
		}
		return nil, forbidden("admins only")
	}
	var res EraseResult
	var keys, sessions []string
	err := s.write(ctx, actor, func(tx *Tx) error {
		u, err := take[store.User](tx.db, id, "user")
		if err != nil {
			return err
		}
		if u.ErasedAt != nil {
			return conflict("this user has already been erased", nil)
		}
		if u.Role == string(authz.Admin) && u.Active {
			last, err := isLastAdmin(tx.db, u.ID)
			if err != nil {
				return err
			}
			if last {
				return conflict("this is the last active admin; promote someone else first", nil)
			}
		}
		var owned int64
		if err := tx.db.Model(&store.Event{}).Where("owner_id = ? AND deleted_at IS NULL", id).Count(&owned).Error; err != nil {
			return err
		}
		if owned > 0 {
			if transferTo == "" {
				return invalidField("transferTo", "this user owns %d events; choose who receives them", owned)
			}
			if transferTo == id {
				return invalidField("transferTo", "choose a different user")
			}
			n, err := s.transferEvents(tx, id, transferTo)
			if err != nil {
				return err
			}
			res.EventsTransferred = n
		}

		// Comments: hard-delete, except that a comment others replied to
		// keeps an empty, deleted row so the thread stays intact.
		var comments []store.Comment
		if err := tx.db.Where("author_id = ?", id).Find(&comments).Error; err != nil {
			return err
		}
		var commentIDs, keepIDs, dropIDs []string
		for _, c := range comments {
			commentIDs = append(commentIDs, c.ID)
		}
		replied := map[string]bool{}
		if len(commentIDs) > 0 {
			var parents []string
			if err := tx.db.Model(&store.Comment{}).Where("parent_id IN ? AND author_id <> ?", commentIDs, id).Distinct().Pluck("parent_id", &parents).Error; err != nil {
				return err
			}
			for _, p := range parents {
				replied[p] = true
			}
		}
		for _, c := range comments {
			if replied[c.ID] {
				keepIDs = append(keepIDs, c.ID)
			} else {
				dropIDs = append(dropIDs, c.ID)
			}
		}
		if len(keepIDs) > 0 {
			if err := tx.db.Model(&store.Comment{}).Where("id IN ?", keepIDs).
				Updates(map[string]any{"body": "", "deleted_at": gorm.Expr("COALESCE(deleted_at, ?)", tx.now), "updated_at": tx.now}).Error; err != nil {
				return err
			}
		}

		// Attachments the user uploaded, plus any left on their comments.
		var atts []store.Attachment
		q := tx.db.Where("uploader_id = ?", id)
		if len(commentIDs) > 0 {
			q = tx.db.Where("uploader_id = ? OR comment_id IN ?", id, commentIDs)
		}
		if err := q.Find(&atts).Error; err != nil {
			return err
		}
		var attIDs []string
		for _, a := range atts {
			attIDs = append(attIDs, a.ID)
			keys = append(keys, a.StorageKey, deref(a.ThumbKey))
		}
		if len(attIDs) > 0 {
			if err := tx.db.Where("id IN ?", attIDs).Delete(&store.Attachment{}).Error; err != nil {
				return err
			}
		}
		if len(dropIDs) > 0 {
			if err := tx.db.Where("id IN ?", dropIDs).Delete(&store.Comment{}).Error; err != nil {
				return err
			}
		}

		// Suggestions the user authored.
		var sgs []store.Suggestion
		if err := tx.db.Where("author_id = ?", id).Find(&sgs).Error; err != nil {
			return err
		}
		var sgIDs []string
		var withdrawn []SuggestionDTO
		for _, sg := range sgs {
			sgIDs = append(sgIDs, sg.ID)
			if sg.Status != store.SuggestionOpen {
				continue
			}
			ev, err := s.loadEvent(tx.db, sg.EventID, true)
			if err != nil {
				return err
			}
			sg.Status = store.SuggestionWithdrawn
			dtos, err := s.suggestionDTOs(tx.db, tx.actor, []store.Suggestion{sg}, ev)
			if err != nil {
				return err
			}
			dtos[0].Author = UserRef{ID: id, Name: ErasedName}
			withdrawn = append(withdrawn, dtos[0])
		}
		if len(sgIDs) > 0 {
			if err := tx.db.Where("id IN ?", sgIDs).Delete(&store.Suggestion{}).Error; err != nil {
				return err
			}
		}

		// orSubjects widens q to rows about any of their erased content.
		orSubjects := func(q *gorm.DB) *gorm.DB {
			for _, sub := range []struct {
				typ string
				ids []string
			}{{"comment", commentIDs}, {"suggestion", sgIDs}, {"attachment", attIDs}} {
				if len(sub.ids) > 0 {
					q = q.Or("subject_type = ? AND subject_id IN ?", sub.typ, sub.ids)
				}
			}
			return q
		}

		// Inbox items addressed to them or about content that is now gone;
		// elsewhere they stop being named as the actor.
		nq := tx.db.Where("user_id = ?", id)
		nq = orSubjects(nq)
		if err := nq.Delete(&store.Notification{}).Error; err != nil {
			return err
		}
		if err := tx.db.Model(&store.Notification{}).Where("actor_id = ?", id).Update("actor_id", nil).Error; err != nil {
			return err
		}

		// Audit rows keep who did what, but not the content.
		aq := tx.db.Model(&store.Activity{}).Where("subject_type = ? AND subject_id = ?", "user", id)
		aq = orSubjects(aq)
		if err := aq.Updates(map[string]any{"before_json": "null", "after_json": "null"}).Error; err != nil {
			return err
		}

		if err := tx.db.Model(&store.Session{}).Where("user_id = ?", id).Pluck("id", &sessions).Error; err != nil {
			return err
		}
		for _, m := range []any{&store.UserPreference{}, &store.FeedToken{}, &store.Session{}} {
			if err := tx.db.Where("user_id = ?", id).Delete(m).Error; err != nil {
				return err
			}
		}
		if err := tx.db.Where("audience_user_id = ?", id).Delete(&store.ChangeLog{}).Error; err != nil {
			return err
		}

		now := tx.now
		if err := tx.db.Model(&store.User{}).Where("id = ?", id).Updates(map[string]any{
			"oidc_issuer": "", "oidc_subject": "erased:" + id, "email": "", "display_name": ErasedName, "avatar_url": "",
			"groups_json": "[]", "active": false, "role_source": store.RoleSourceManual, "erased_at": now, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.retitleForPerson(id); err != nil {
			return err
		}
		res.CommentsRemoved, res.AttachmentsRemoved, res.SuggestionsRemoved = len(comments), len(atts), len(sgs)
		if err := tx.record("user.erased", "user", id, nil, nil, res); err != nil {
			return err
		}

		// Tell open browsers: removed content disappears, and the user's
		// own streams close.
		for _, c := range comments {
			if c.DeletedAt == nil {
				if err := tx.emit("comment.deleted", c.ID, 0, map[string]string{"id": c.ID, "eventId": c.EventID}, ""); err != nil {
					return err
				}
			}
		}
		for _, a := range atts {
			if a.DeletedAt == nil {
				if err := tx.emit("attachment.removed", a.ID, 0, map[string]any{"id": a.ID, "eventId": a.EventID, "commentId": a.CommentID}, ""); err != nil {
					return err
				}
			}
		}
		for _, d := range withdrawn {
			if err := tx.emit("suggestion.resolved", d.ID, 0, d, ""); err != nil {
				return err
			}
		}
		return tx.emit("user.role_changed", id, 0, map[string]any{"userId": id, "role": u.Role, "active": false}, "")
	})
	if err != nil {
		return nil, err
	}
	if s.Sessions != nil && s.Sessions.OnRevoke != nil && len(sessions) > 0 {
		// The rows went with the transaction; this closes open SSE streams.
		s.Sessions.OnRevoke(sessions)
	}
	s.gcBlobs(ctx, keys)
	return &res, nil
}
