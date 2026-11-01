package domain

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

const (
	maxNamePart    = 60
	maxPeople      = 2000
	maxOpenPending = 20
)

// PersonDTO is someone events can mention who has no account.
type PersonDTO struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name" doc:"First and last name together"`
	FirstName   string                  `json:"firstName"`
	LastName    string                  `json:"lastName" doc:"Empty for people added before last names were asked for"`
	Status      string                  `json:"status" enum:"pending,active"`
	CreatedBy   *UserRef                `json:"createdBy"`
	CreatedAt   time.Time               `json:"createdAt"`
	Permissions authz.PersonPermissions `json:"permissions"`
}

// PeopleDTO is the People page: registered users and people without accounts.
type PeopleDTO struct {
	Users  []UserRef   `json:"users"`
	People []PersonDTO `json:"people"`
}

// PersonInput creates or renames a person.
type PersonInput struct {
	FirstName string `json:"firstName" maxLength:"60"`
	LastName  string `json:"lastName" maxLength:"60"`
}

func authzPerson(p *store.Person) authz.Person {
	c := ""
	if p.CreatedByID != nil {
		c = *p.CreatedByID
	}
	return authz.Person{CreatedByID: c, Pending: p.Status == store.PersonPending}
}

func (s *Service) personDTOs(db *gorm.DB, actor authz.Actor, rows []store.Person) ([]PersonDTO, error) {
	var ids []string
	for _, p := range rows {
		if p.CreatedByID != nil {
			ids = append(ids, *p.CreatedByID)
		}
	}
	refs, err := userRefs(db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]PersonDTO, len(rows))
	for i := range rows {
		p := &rows[i]
		d := PersonDTO{ID: p.ID, Name: p.Name, FirstName: p.FirstName, LastName: p.LastName, Status: p.Status, CreatedAt: p.CreatedAt, Permissions: authz.PersonPerms(actor, authzPerson(p))}
		if p.CreatedByID != nil {
			if r, ok := refs[*p.CreatedByID]; ok {
				d.CreatedBy = &r
			}
		}
		out[i] = d
	}
	return out, nil
}

// emitPeople tells open browsers to refetch the people list and events,
// whose member names and references may have changed.
func (tx *Tx) emitPeople(id string) error {
	return tx.emit("person.changed", id, 0, map[string]string{"id": id}, "")
}

// ListPeople returns the active users and the people without accounts. Other
// writers' pending submissions are visible only to admins and their author.
func (s *Service) ListPeople(ctx context.Context, actor authz.Actor) (*PeopleDTO, error) {
	users, err := s.Directory(ctx, actor)
	if err != nil {
		return nil, err
	}
	db := s.DB.R.WithContext(ctx)
	q := db.Where("bound_user_id IS NULL")
	if !authz.CanManagePeople(actor) {
		q = q.Where("status = ? OR created_by_id = ?", store.PersonActive, actor.ID)
	}
	var rows []store.Person
	if err := q.Order("name, id").Limit(maxPeople).Find(&rows).Error; err != nil {
		return nil, err
	}
	people, err := s.personDTOs(db, actor, rows)
	if err != nil {
		return nil, err
	}
	return &PeopleDTO{Users: users, People: people}, nil
}

// CountPendingPeople is the number of submissions waiting for an admin.
func (s *Service) CountPendingPeople(ctx context.Context, actor authz.Actor) (int64, error) {
	if !authz.CanManagePeople(actor) {
		return 0, forbidden("admins only")
	}
	var n int64
	err := s.DB.R.WithContext(ctx).Model(&store.Person{}).Where("status = ? AND bound_user_id IS NULL", store.PersonPending).Count(&n).Error
	return n, err
}

// checkPersonName trims and validates a first and last name and returns
// them with the combined display name, which must not duplicate another
// person's (ignoring case).
func checkPersonName(db *gorm.DB, in PersonInput, exceptID string) (first, last, name string, err error) {
	first = strings.Join(strings.Fields(in.FirstName), " ")
	last = strings.Join(strings.Fields(in.LastName), " ")
	if first == "" || utf8.RuneCountInString(first) > maxNamePart {
		return "", "", "", invalidField("firstName", "first name must be 1–%d characters", maxNamePart)
	}
	if last == "" || utf8.RuneCountInString(last) > maxNamePart {
		return "", "", "", invalidField("lastName", "last name must be 1–%d characters", maxNamePart)
	}
	name = first + " " + last
	var dup int64
	if err := db.Model(&store.Person{}).
		Where("LOWER(name) = ? AND id <> ? AND bound_user_id IS NULL", strings.ToLower(name), exceptID).Count(&dup).Error; err != nil {
		return "", "", "", err
	}
	if dup > 0 {
		return "", "", "", invalidField("lastName", "someone with this name already exists")
	}
	return first, last, name, nil
}

func (s *Service) onePersonDTO(tx *Tx, p store.Person) (*PersonDTO, error) {
	dtos, err := s.personDTOs(tx.db, tx.actor, []store.Person{p})
	if err != nil {
		return nil, err
	}
	return &dtos[0], nil
}

// CreatePerson adds a person. An admin's is active at once; a writer's waits
// for approval but the writer can already use it.
func (s *Service) CreatePerson(ctx context.Context, actor authz.Actor, in PersonInput) (*PersonDTO, error) {
	if !authz.CanCreatePerson(actor) {
		return nil, forbidden("not allowed")
	}
	var out *PersonDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		first, last, name, err := checkPersonName(tx.db, in, "")
		if err != nil {
			return err
		}
		status := store.PersonPending
		if authz.CanManagePeople(actor) {
			status = store.PersonActive
		} else {
			var mine int64
			if err := tx.db.Model(&store.Person{}).Where("created_by_id = ? AND status = ?", actor.ID, store.PersonPending).Count(&mine).Error; err != nil {
				return err
			}
			if mine >= maxOpenPending {
				return limit("you have too many people waiting for approval")
			}
		}
		var total int64
		if err := tx.db.Model(&store.Person{}).Count(&total).Error; err != nil {
			return err
		}
		if total >= maxPeople {
			return limit("the people list is full")
		}
		by := actor.ID
		p := store.Person{ID: store.NewID(), Name: name, FirstName: first, LastName: last, Status: status, CreatedByID: &by, CreatedAt: tx.now, UpdatedAt: tx.now}
		if err := tx.db.Create(&p).Error; err != nil {
			return err
		}
		if out, err = s.onePersonDTO(tx, p); err != nil {
			return err
		}
		if err := tx.record("person.created", "person", p.ID, nil, nil, out); err != nil {
			return err
		}
		return tx.emitPeople(p.ID)
	})
	return out, err
}

// loadPerson takes a person who has not been bound to a user.
func loadPerson(db *gorm.DB, id string) (store.Person, error) {
	p, err := take[store.Person](db, id, "person")
	if err == nil && p.BoundUserID != nil {
		return p, notFound("person")
	}
	return p, err
}

// UpdatePerson renames a person and refreshes the titles that mention them.
func (s *Service) UpdatePerson(ctx context.Context, actor authz.Actor, id string, in PersonInput) (*PersonDTO, error) {
	var out *PersonDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		p, err := loadPerson(tx.db, id)
		if err != nil {
			return err
		}
		if !authz.CanEditPerson(actor, authzPerson(&p)) {
			return forbidden("not allowed")
		}
		first, last, name, err := checkPersonName(tx.db, in, p.ID)
		if err != nil {
			return err
		}
		if first == p.FirstName && last == p.LastName {
			out, err = s.onePersonDTO(tx, p)
			return err
		}
		before := p.Name
		p.Name, p.FirstName, p.LastName, p.UpdatedAt = name, first, last, tx.now
		if err := tx.db.Model(&store.Person{}).Where("id = ?", p.ID).
			Updates(map[string]any{"name": name, "first_name": first, "last_name": last, "updated_at": tx.now}).Error; err != nil {
			return err
		}
		if err := tx.retitleForPerson(p.ID); err != nil {
			return err
		}
		if out, err = s.onePersonDTO(tx, p); err != nil {
			return err
		}
		if err := tx.record("person.renamed", "person", p.ID, nil, map[string]string{"name": before}, map[string]string{"name": name}); err != nil {
			return err
		}
		return tx.emitPeople(p.ID)
	})
	return out, err
}

// ApprovePerson makes a pending person visible to everyone.
func (s *Service) ApprovePerson(ctx context.Context, actor authz.Actor, id string) (*PersonDTO, error) {
	if !authz.CanManagePeople(actor) {
		return nil, forbidden("admins only")
	}
	var out *PersonDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		p, err := loadPerson(tx.db, id)
		if err != nil {
			return err
		}
		if p.Status == store.PersonPending {
			p.Status, p.UpdatedAt = store.PersonActive, tx.now
			if err := tx.db.Model(&store.Person{}).Where("id = ?", p.ID).Updates(map[string]any{"status": p.Status, "updated_at": tx.now}).Error; err != nil {
				return err
			}
			if err := tx.record("person.approved", "person", p.ID, nil, nil, map[string]string{"name": p.Name}); err != nil {
				return err
			}
			if err := tx.emitPeople(p.ID); err != nil {
				return err
			}
		}
		out, err = s.onePersonDTO(tx, p)
		return err
	})
	return out, err
}

// DeletePerson removes a person (rejecting them, when pending) and takes them
// out of every event that mentioned them.
func (s *Service) DeletePerson(ctx context.Context, actor authz.Actor, id string) error {
	if !authz.CanManagePeople(actor) {
		return forbidden("admins only")
	}
	return s.write(ctx, actor, func(tx *Tx) error {
		p, err := loadPerson(tx.db, id)
		if err != nil {
			return err
		}
		if err := tx.rewritePersonRefs(p.ID, ""); err != nil {
			return err
		}
		if err := tx.db.Where("id = ?", p.ID).Delete(&store.Person{}).Error; err != nil {
			return err
		}
		if err := tx.record("person.deleted", "person", p.ID, nil, map[string]string{"name": p.Name, "status": p.Status}, nil); err != nil {
			return err
		}
		return tx.emitPeople(p.ID)
	})
}

// BindPerson links a person to the user they became. Every event reference
// to the person is rewritten to the user; the person row stays as a pointer
// so stale clients and pending suggestions still resolve.
func (s *Service) BindPerson(ctx context.Context, actor authz.Actor, id, userID string) error {
	if !authz.CanManagePeople(actor) {
		return forbidden("admins only")
	}
	return s.write(ctx, actor, func(tx *Tx) error {
		p, err := loadPerson(tx.db, id)
		if err != nil {
			return err
		}
		u, err := take[store.User](tx.db, userID, "user")
		if err != nil {
			return err
		}
		if u.ID == store.SystemUserID || u.ErasedAt != nil {
			return invalidField("userId", "choose a registered user")
		}
		if err := tx.rewritePersonRefs(p.ID, u.ID); err != nil {
			return err
		}
		if err := tx.db.Model(&store.Person{}).Where("id = ?", p.ID).
			Updates(map[string]any{"bound_user_id": u.ID, "bound_at": tx.now, "status": store.PersonActive, "updated_at": tx.now}).Error; err != nil {
			return err
		}
		if err := tx.record("person.bound", "person", p.ID, nil, map[string]string{"name": p.Name}, map[string]string{"userId": u.ID}); err != nil {
			return err
		}
		return tx.emitPeople(p.ID)
	})
}

// rewritePersonRefs replaces personID in the field values of every event and
// occurrence with userID, or removes it when userID is empty, then refreshes
// the generated titles. Touched events get a new version so open editors see
// a conflict rather than saving the old reference back.
func (tx *Tx) rewritePersonRefs(personID, userID string) error {
	like := "%" + personID + "%"
	touched := map[string]bool{}
	var evs []store.Event
	err := tx.db.Where("fields_json LIKE ?", like).FindInBatches(&evs, 200, func(db *gorm.DB, _ int) error {
		for _, e := range evs {
			fields, changed := swapPersonRef(e.FieldsJSON, personID, userID)
			if !changed {
				continue
			}
			if e.EventTypeID != nil {
				touched[*e.EventTypeID] = true
			}
			if err := db.Model(&store.Event{}).Where("id = ?", e.ID).Updates(map[string]any{"fields_json": fields, "version": e.Version + 1, "updated_at": tx.now}).Error; err != nil {
				return err
			}
		}
		return nil
	}).Error
	if err != nil {
		return err
	}
	var ovs []store.EventOverride
	if err := tx.db.Where("fields_json LIKE ?", like).Find(&ovs).Error; err != nil {
		return err
	}
	for _, ov := range ovs {
		fields, changed := swapPersonRef(ov.FieldsJSON, personID, userID)
		if !changed {
			continue
		}
		if ov.EventTypeID != nil {
			touched[*ov.EventTypeID] = true
		}
		if err := tx.db.Model(&store.EventOverride{}).Where("id = ?", ov.ID).Updates(map[string]any{"fields_json": fields, "updated_at": tx.now}).Error; err != nil {
			return err
		}
	}
	for id := range touched {
		var t store.EventType
		if err := tx.db.Where("id = ?", id).Take(&t).Error; err != nil {
			if isNotFound(err) {
				continue
			}
			return err
		}
		if err := tx.retitleType(t, ""); err != nil {
			return err
		}
	}
	return nil
}

// swapPersonRef rewrites encoded field values, replacing personID in every
// list with userID (dropping duplicates) or removing it when userID is empty.
func swapPersonRef(encoded, personID, userID string) (string, bool) {
	vals := parseFieldValues(encoded)
	changed := false
	for k, v := range vals {
		list, ok := v.([]any)
		if !ok {
			continue
		}
		var out []any
		seen := map[any]bool{}
		for _, e := range list {
			if e == personID {
				changed = true
				if userID == "" {
					continue
				}
				e = userID
			}
			if seen[e] {
				continue
			}
			seen[e] = true
			out = append(out, e)
		}
		if len(out) == 0 {
			delete(vals, k)
		} else {
			vals[k] = out
		}
	}
	if !changed {
		return encoded, false
	}
	b, _ := json.Marshal(vals)
	return string(b), true
}

// resolvePeople maps the member ids an event refers to onto canonical ones:
// users stay, people who have since been bound become their user, other
// people stay. ok is false if any id is neither a user nor a person.
func resolvePeople(db *gorm.DB, ids []string) (out []string, ok bool, err error) {
	var users []store.User
	if err := db.Select("id").Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, false, err
	}
	known := map[string]string{}
	for _, u := range users {
		known[u.ID] = u.ID
	}
	var rest []string
	for _, id := range ids {
		if known[id] == "" {
			rest = append(rest, id)
		}
	}
	if len(rest) > 0 {
		var people []store.Person
		if err := db.Where("id IN ?", rest).Find(&people).Error; err != nil {
			return nil, false, err
		}
		for _, p := range people {
			known[p.ID] = p.ID
			if p.BoundUserID != nil {
				known[p.ID] = *p.BoundUserID
			}
		}
	}
	for _, id := range ids {
		if known[id] == "" {
			return nil, false, nil
		}
		out = append(out, known[id])
	}
	return uniq(out), true, nil
}

// memberLabels maps user and person ids to display names. A bound person
// shows as their user.
func memberLabels(db *gorm.DB, ids []string) (map[string]string, error) {
	names := map[string]string{}
	ids = uniq(ids)
	if len(ids) == 0 {
		return names, nil
	}
	var users []store.User
	if err := db.Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	for i := range users {
		names[users[i].ID] = refOf(&users[i]).Name
	}
	var people []store.Person
	if err := db.Where("id IN ?", ids).Find(&people).Error; err != nil {
		return nil, err
	}
	var bound []string
	for _, p := range people {
		names[p.ID] = p.Name
		if p.BoundUserID != nil {
			bound = append(bound, *p.BoundUserID)
		}
	}
	if len(bound) > 0 {
		var us []store.User
		if err := db.Where("id IN ?", uniq(bound)).Find(&us).Error; err != nil {
			return nil, err
		}
		byID := map[string]string{}
		for i := range us {
			byID[us[i].ID] = refOf(&us[i]).Name
		}
		for _, p := range people {
			if p.BoundUserID != nil && byID[*p.BoundUserID] != "" {
				names[p.ID] = byID[*p.BoundUserID]
			}
		}
	}
	return names, nil
}
