package domain

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

const settingsKey = "instance"

// Settings are the instance settings edited on the admin Settings page.
type Settings struct {
	CalendarName            string   `json:"calendarName" maxLength:"100"`
	DefaultTimeZone         string   `json:"defaultTimeZone" maxLength:"64"`
	DefaultRole             string   `json:"defaultRole" enum:"viewer,writer,admin"`
	AccentColor             string   `json:"accentColor" maxLength:"7"`
	EnabledThemes           []string `json:"enabledThemes" maxItems:"100"`
	AttachmentMaxSize       int64    `json:"attachmentMaxSize" minimum:"1"`
	AttachmentMaxPerEvent   int      `json:"attachmentMaxPerEvent" minimum:"0"`
	AttachmentMaxPerComment int      `json:"attachmentMaxPerComment" minimum:"0"`
	AttachmentQuota         int64    `json:"attachmentQuota" minimum:"0"`
	StorageUsed             int64    `json:"storageUsed" readOnly:"true"`
	LogoKey                 string   `json:"-"`
}

// storedSettings is what the settings row holds.
type storedSettings struct {
	Settings
	LogoKey string `json:"logoKey,omitempty"`
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (s *Service) defaultSettings() Settings {
	return Settings{
		CalendarName:            "SharedCal",
		DefaultTimeZone:         "UTC",
		DefaultRole:             string(s.Opts.Roles.DefaultRole),
		EnabledThemes:           []string{"default"},
		AttachmentMaxSize:       s.Opts.AttachmentMaxSize,
		AttachmentMaxPerEvent:   s.Opts.AttachmentMaxPerEvent,
		AttachmentMaxPerComment: s.Opts.AttachmentMaxPerComment,
		AttachmentQuota:         s.Opts.AttachmentQuota,
	}
}

func (s *Service) loadSettings(db *gorm.DB) (Settings, error) {
	out := s.defaultSettings()
	var row store.Setting
	err := byKey(db, settingsKey).Take(&row).Error
	if isNotFound(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	var st storedSettings
	st.Settings = out
	if err := json.Unmarshal([]byte(row.ValueJSON), &st); err != nil {
		return out, err
	}
	st.Settings.LogoKey = st.LogoKey
	if !authz.Role(st.DefaultRole).Valid() {
		st.DefaultRole = out.DefaultRole
	}
	return st.Settings, nil
}

func (s *Service) saveSettings(tx *Tx, st Settings) error {
	data, err := json.Marshal(storedSettings{Settings: st, LogoKey: st.LogoKey})
	if err != nil {
		return err
	}
	row := store.Setting{Key: settingsKey, ValueJSON: string(data), CreatedAt: tx.now, UpdatedAt: tx.now}
	var existing store.Setting
	if err := byKey(tx.db, settingsKey).Take(&existing).Error; err == nil {
		row.CreatedAt = existing.CreatedAt
		return tx.db.Save(&row).Error
	} else if !isNotFound(err) {
		return err
	}
	return tx.db.Create(&row).Error
}

// byKey filters settings rows by key. "key" is reserved in MySQL, so the
// column goes through GORM's quoting rather than a raw SQL fragment.
func byKey(db *gorm.DB, key string) *gorm.DB {
	return db.Where(clause.Eq{Column: clause.Column{Name: "key"}, Value: key})
}

func (s *Service) settingDefaultRole(db *gorm.DB) (authz.Role, bool) {
	st, err := s.loadSettings(db)
	if err != nil {
		return "", false
	}
	r := authz.Role(st.DefaultRole)
	return r, r.Valid()
}

// PublicSettings is what every signed-in user sees about the instance.
type PublicSettings struct {
	CalendarName    string       `json:"calendarName"`
	DefaultTimeZone string       `json:"defaultTimeZone"`
	AccentColor     string       `json:"accentColor"`
	HasLogo         bool         `json:"hasLogo"`
	LogoURL         string       `json:"logoUrl"`
	Limits          UploadLimits `json:"limits"`
}

// UploadLimits are the effective attachment limits.
type UploadLimits struct {
	MaxSize       int64 `json:"maxSize"`
	MaxPerEvent   int   `json:"maxPerEvent"`
	MaxPerComment int   `json:"maxPerComment"`
}

// Public returns the instance settings visible to everyone.
func (s *Service) Public(ctx context.Context) (PublicSettings, error) {
	st, err := s.loadSettings(s.DB.R.WithContext(ctx))
	if err != nil {
		return PublicSettings{}, err
	}
	p := PublicSettings{
		CalendarName: st.CalendarName, DefaultTimeZone: st.DefaultTimeZone, AccentColor: st.AccentColor,
		HasLogo: st.LogoKey != "",
		Limits:  UploadLimits{MaxSize: st.AttachmentMaxSize, MaxPerEvent: st.AttachmentMaxPerEvent, MaxPerComment: st.AttachmentMaxPerComment},
	}
	if p.HasLogo {
		p.LogoURL = "/branding/logo?v=" + st.LogoKey[len(st.LogoKey)-12:]
	}
	return p, nil
}

// GetSettings returns the full settings (admins).
func (s *Service) GetSettings(ctx context.Context, actor authz.Actor) (*Settings, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	db := s.DB.R.WithContext(ctx)
	st, err := s.loadSettings(db)
	if err != nil {
		return nil, err
	}
	if st.StorageUsed, err = storageUsed(db); err != nil {
		return nil, err
	}
	return &st, nil
}

// UpdateSettings validates and saves instance settings.
func (s *Service) UpdateSettings(ctx context.Context, actor authz.Actor, in Settings) (*Settings, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	var fe fieldErrors
	in.CalendarName = strings.TrimSpace(in.CalendarName)
	if in.CalendarName == "" || utf8.RuneCountInString(in.CalendarName) > 100 {
		fe.add("calendarName", "calendar name must be 1–100 characters")
	}
	if _, err := loadLocation(in.DefaultTimeZone); err != nil {
		fe.add("defaultTimeZone", "unknown time zone")
	}
	if !authz.Role(in.DefaultRole).Valid() {
		fe.add("defaultRole", "unknown role")
	}
	if in.AccentColor != "" && !hexColor.MatchString(in.AccentColor) {
		fe.add("accentColor", "accent color must be #rrggbb")
	}
	if in.AttachmentMaxSize <= 0 || in.AttachmentMaxSize > s.Opts.AttachmentMaxSize {
		// The env value is the ceiling: proxies and the HTTP layer are sized for it.
		fe.add("attachmentMaxSize", "must be between 1 byte and the server limit (%d bytes)", s.Opts.AttachmentMaxSize)
	}
	if in.AttachmentMaxPerEvent < 0 || in.AttachmentMaxPerEvent > 1000 {
		fe.add("attachmentMaxPerEvent", "must be 0–1000")
	}
	if in.AttachmentMaxPerComment < 0 || in.AttachmentMaxPerComment > 100 {
		fe.add("attachmentMaxPerComment", "must be 0–100")
	}
	if in.AttachmentQuota < 0 {
		fe.add("attachmentQuota", "must not be negative")
	}
	if err := fe.err(); err != nil {
		return nil, err
	}
	var out Settings
	err := s.write(ctx, actor, func(tx *Tx) error {
		cur, err := s.loadSettings(tx.db)
		if err != nil {
			return err
		}
		known, err := s.themeIDs(tx.db)
		if err != nil {
			return err
		}
		enabled := []string{"default"}
		for _, id := range uniq(in.EnabledThemes) {
			if id != "default" && known[id] {
				enabled = append(enabled, id)
			}
		}
		in.EnabledThemes = enabled
		in.LogoKey = cur.LogoKey
		in.StorageUsed = 0
		if err := s.saveSettings(tx, in); err != nil {
			return err
		}
		if err := tx.record("settings.updated", "settings", settingsKey, nil, cur, in); err != nil {
			return err
		}
		out = in
		out.StorageUsed, err = storageUsed(tx.db)
		if err != nil {
			return err
		}
		return tx.emit("settings.changed", settingsKey, 0, struct{}{}, "")
	})
	return &out, err
}

func storageUsed(db *gorm.DB) (int64, error) {
	var total struct{ N *int64 }
	err := db.Model(&store.Attachment{}).Select("SUM(size_bytes) AS n").Where("scan_status <> ?", store.ScanInfected).Scan(&total).Error
	if err != nil || total.N == nil {
		return 0, err
	}
	return *total.N, nil
}

// StorageUsed reports the bytes of attachments stored (for metrics).
func (s *Service) StorageUsed(ctx context.Context) (int64, error) {
	return storageUsed(s.DB.R.WithContext(ctx))
}
