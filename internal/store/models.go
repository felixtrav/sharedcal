// Package store holds the GORM models, database opening and migrations.
//
// The models mirror the versioned SQL migrations in /migrations. Production
// never calls AutoMigrate; TestModelsMatchMigrations keeps the two in sync.
package store

import (
	"time"

	"github.com/google/uuid"
)

// NewID returns a new UUIDv7 string: time-ordered, safe to expose, and valid
// as an iCalendar UID.
// SystemUserID identifies the reserved inactive user that owns events of
// ownerless types. It never signs in and is hidden from user lists.
const SystemUserID = "00000000-0000-0000-0000-000000000000"

func NewID() string { return uuid.Must(uuid.NewV7()).String() }

// Enumerations stored as short strings with check constraints.
const (
	RoleSourceIDP    = "idp"
	RoleSourceManual = "manual"

	StatusConfirmed = "confirmed"
	StatusTentative = "tentative"

	ScanPending  = "pending"
	ScanClean    = "clean"
	ScanInfected = "infected"
	ScanSkipped  = "skipped"

	SuggestionEdit   = "edit"
	SuggestionDelete = "delete"

	SuggestionOpen              = "open"
	SuggestionAccepted          = "accepted"
	SuggestionPartiallyAccepted = "partially_accepted"
	SuggestionRejected          = "rejected"
	SuggestionWithdrawn         = "withdrawn"
	SuggestionClosed            = "closed"
)

type User struct {
	ID          string `gorm:"primaryKey;size:36"`
	OIDCIssuer  string `gorm:"column:oidc_issuer;size:255;not null;uniqueIndex:ux_users_oidc,priority:1"`
	OIDCSubject string `gorm:"column:oidc_subject;size:255;not null;uniqueIndex:ux_users_oidc,priority:2"`
	Email       string `gorm:"size:320;not null;default:''"`
	DisplayName string `gorm:"size:200;not null;default:''"`
	AvatarURL   string `gorm:"column:avatar_url;size:2048;not null;default:''"`
	Role        string `gorm:"size:16;not null"`
	RoleSource  string `gorm:"size:16;not null"`
	GroupsJSON  string `gorm:"column:groups_json;type:text;not null"`
	Active      bool   `gorm:"not null"`
	LastLoginAt *time.Time
	// ErasedAt is set once an admin has erased the user's personal data.
	ErasedAt  *time.Time
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

type UserPreference struct {
	UserID      string    `gorm:"primaryKey;size:36"`
	ThemeID     string    `gorm:"size:64;not null"`
	ColorMode   string    `gorm:"size:16;not null"`
	TimeZone    string    `gorm:"size:64;not null;default:''"`
	WeekStart   int       `gorm:"not null"`
	Clock24h    bool      `gorm:"column:clock_24h;not null"`
	DefaultView string    `gorm:"size:16;not null"`
	Density     string    `gorm:"size:16;not null"`
	CreatedAt   time.Time `gorm:"not null"`
	UpdatedAt   time.Time `gorm:"not null"`
}

type Session struct {
	// ID is the SHA-256 hash (hex) of the cookie value; the raw ID is never stored.
	ID            string    `gorm:"primaryKey;size:64"`
	UserID        string    `gorm:"size:36;not null;index:ix_sessions_user"`
	ExpiresAt     time.Time `gorm:"not null"`
	IdleExpiresAt time.Time `gorm:"not null"`
	LastSeenAt    time.Time `gorm:"not null"`
	IP            string    `gorm:"size:64;not null;default:''"`
	UserAgent     string    `gorm:"size:512;not null;default:''"`
	CreatedAt     time.Time `gorm:"not null"`
	UpdatedAt     time.Time `gorm:"not null"`
}

type Event struct {
	ID          string `gorm:"primaryKey;size:36"`
	OwnerID     string `gorm:"size:36;not null;index:ix_events_owner"`
	Title       string `gorm:"size:200;not null"`
	Description string `gorm:"type:text;not null"`
	Location    string `gorm:"size:1000;not null;default:''"`
	// EventTypeID, when set, makes the event typed: FieldsJSON holds values
	// for the type's fields and Description stays empty.
	EventTypeID *string   `gorm:"size:36;index:ix_events_type"`
	FieldsJSON  string    `gorm:"column:fields_json;type:text;not null"`
	StartAt     time.Time `gorm:"not null;index:ix_events_range,priority:1"`
	EndAt       time.Time `gorm:"not null;index:ix_events_range,priority:2"`
	AllDay      bool      `gorm:"not null"`
	TimeZone    string    `gorm:"size:64;not null"`
	RRule       string    `gorm:"column:rrule;size:1000;not null;default:''"`
	// SourceUID is the iCalendar UID the event was imported from; re-imports skip UIDs already present.
	SourceUID string `gorm:"column:source_uid;size:255;not null;default:'';index:ix_events_source_uid"`
	// SeriesEndAt is the end of the last occurrence for recurring events
	// (nil = unbounded); it lets range queries skip finished series.
	SeriesEndAt *time.Time
	ExdatesJSON string     `gorm:"column:exdates_json;type:text;not null"`
	Status      string     `gorm:"size:16;not null"`
	Version     int64      `gorm:"not null"`
	DeletedAt   *time.Time `gorm:"index:ix_events_deleted"`
	DeletedByID *string    `gorm:"size:36"`
	CreatedAt   time.Time  `gorm:"not null"`
	UpdatedAt   time.Time  `gorm:"not null"`
}

// EventOverride replaces or cancels one occurrence of a recurring event
// (RFC 5545 RECURRENCE-ID semantics). Nil fields inherit from the master.
type EventOverride struct {
	ID           string    `gorm:"primaryKey;size:36"`
	EventID      string    `gorm:"size:36;not null;uniqueIndex:ux_overrides_occurrence,priority:1"`
	RecurrenceID time.Time `gorm:"not null;uniqueIndex:ux_overrides_occurrence,priority:2"`
	Title        *string   `gorm:"size:200"`
	Description  *string   `gorm:"type:text"`
	Location     *string   `gorm:"size:1000"`
	StartAt      *time.Time
	EndAt        *time.Time
	AllDay       *bool
	Status       *string `gorm:"size:16"`
	// TypeOverridden makes EventTypeID (nil = generic) and FieldsJSON replace
	// the master's type and field values for this occurrence.
	TypeOverridden bool      `gorm:"not null"`
	EventTypeID    *string   `gorm:"size:36"`
	FieldsJSON     string    `gorm:"column:fields_json;type:text;not null"`
	Cancelled      bool      `gorm:"not null"`
	CreatedAt      time.Time `gorm:"not null"`
	UpdatedAt      time.Time `gorm:"not null"`
}

// EventType is an admin-defined kind of event with a calendar look (icon and
// color slot), creation defaults and a list of predefined fields.
type EventType struct {
	ID            string `gorm:"primaryKey;size:36"`
	Name          string `gorm:"size:64;not null;uniqueIndex:ux_event_types_name"`
	Icon          string `gorm:"size:32;not null"`
	ColorSlot     string `gorm:"size:16;not null"`
	DefaultAllDay bool   `gorm:"not null"`
	// Style is how the calendar draws the type: default, banner, outline or badge.
	Style string `gorm:"size:16;not null;default:'default'"`
	// YearCounter shows the years since the series' first occurrence (birthdays, anniversaries).
	YearCounter bool `gorm:"not null"`
	// DefaultDurationMinutes is the length of new timed events; 0 makes them instants (deadlines).
	// No gorm default tag: it would turn an explicit 0 into 60 on insert.
	DefaultDurationMinutes int    `gorm:"not null"`
	DefaultRRule           string `gorm:"column:default_rrule;size:500;not null;default:''"`
	// TitleTemplate, when set, generates event titles from field values ("{{person}}'s birthday").
	TitleTemplate string `gorm:"size:200;not null;default:''"`
	// NeverRepeats forbids repetition for events of this type; it excludes DefaultRRule.
	NeverRepeats bool `gorm:"not null"`
	// SingleDay limits an always-all-day type to one date. It only applies with DefaultAllDay.
	SingleDay bool `gorm:"not null"`
	// AdminOnly reserves the type for admins: only they can create events of it or switch events to it.
	AdminOnly bool `gorm:"not null"`
	// Ownerless gives the type's events no personal owner: they belong to the calendar (SystemUserID)
	// and only admins edit them.
	Ownerless bool `gorm:"not null"`
	// Instructions are shown to writers while they create an event of this type.
	Instructions string    `gorm:"size:1000;not null;default:''"`
	FieldsJSON   string    `gorm:"column:fields_json;type:text;not null"`
	SortOrder    int       `gorm:"not null"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

type Tag struct {
	ID        string    `gorm:"primaryKey;size:36"`
	Name      string    `gorm:"size:64;not null;uniqueIndex:ux_tags_name"`
	ColorSlot string    `gorm:"size:16;not null"`
	SortOrder int       `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

type EventTag struct {
	EventID  string `gorm:"primaryKey;size:36"`
	TagID    string `gorm:"primaryKey;size:36;index:ix_event_tags_tag"`
	Position int    `gorm:"not null"`
}

type Comment struct {
	ID        string  `gorm:"primaryKey;size:36"`
	EventID   string  `gorm:"size:36;not null;index:ix_comments_event"`
	AuthorID  string  `gorm:"size:36;not null"`
	ParentID  *string `gorm:"size:36"`
	Body      string  `gorm:"type:text;not null"`
	EditedAt  *time.Time
	DeletedAt *time.Time
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

type Attachment struct {
	ID          string  `gorm:"primaryKey;size:36"`
	EventID     string  `gorm:"size:36;not null;index:ix_attachments_event"`
	CommentID   *string `gorm:"size:36;index:ix_attachments_comment"`
	UploaderID  string  `gorm:"size:36;not null"`
	Filename    string  `gorm:"size:255;not null"`
	ContentType string  `gorm:"size:128;not null"`
	SizeBytes   int64   `gorm:"not null"`
	SHA256      string  `gorm:"column:sha256;size:64;not null;index:ix_attachments_sha"`
	StorageKey  string  `gorm:"size:200;not null"`
	Width       *int
	Height      *int
	ThumbKey    *string    `gorm:"size:200"`
	ScanStatus  string     `gorm:"size:16;not null"`
	DeletedAt   *time.Time `gorm:"index:ix_attachments_deleted"`
	CreatedAt   time.Time  `gorm:"not null"`
	UpdatedAt   time.Time  `gorm:"not null"`
}

type Suggestion struct {
	ID          string  `gorm:"primaryKey;size:36"`
	EventID     string  `gorm:"size:36;not null;index:ix_suggestions_event"`
	AuthorID    string  `gorm:"size:36;not null"`
	Kind        string  `gorm:"size:16;not null"`
	PatchJSON   string  `gorm:"column:patch_json;type:text;not null"`
	BaseVersion int64   `gorm:"not null"`
	Status      string  `gorm:"size:24;not null;index:ix_suggestions_status"`
	ReviewerID  *string `gorm:"size:36"`
	ReviewNote  string  `gorm:"type:text;not null"`
	ResolvedAt  *time.Time
	CreatedAt   time.Time `gorm:"not null"`
	UpdatedAt   time.Time `gorm:"not null"`
}

type Notification struct {
	ID          string  `gorm:"primaryKey;size:36"`
	UserID      string  `gorm:"size:36;not null;index:ix_notifications_user,priority:1"`
	Kind        string  `gorm:"size:32;not null"`
	SubjectType string  `gorm:"size:32;not null"`
	SubjectID   string  `gorm:"size:36;not null"`
	EventID     *string `gorm:"size:36"`
	ActorID     *string `gorm:"size:36"`
	ReadAt      *time.Time
	CreatedAt   time.Time `gorm:"not null;index:ix_notifications_user,priority:2"`
	UpdatedAt   time.Time `gorm:"not null"`
}

// Activity is the append-only audit log.
type Activity struct {
	ID          string    `gorm:"primaryKey;size:36"`
	ActorID     *string   `gorm:"size:36;index:ix_activity_actor"`
	Action      string    `gorm:"size:64;not null"`
	SubjectType string    `gorm:"size:32;not null"`
	SubjectID   string    `gorm:"size:36;not null"`
	EventID     *string   `gorm:"size:36;index:ix_activity_event"`
	BeforeJSON  string    `gorm:"column:before_json;type:text;not null"`
	AfterJSON   string    `gorm:"column:after_json;type:text;not null"`
	CreatedAt   time.Time `gorm:"not null;index:ix_activity_created"`
}

func (Activity) TableName() string { return "activity" }

// ChangeLog is the SSE replay buffer, pruned after 24 hours.
type ChangeLog struct {
	Seq       int64  `gorm:"primaryKey;autoIncrement"`
	Type      string `gorm:"size:32;not null"`
	SubjectID string `gorm:"size:36;not null"`
	// AudienceUserID restricts delivery to one user (inbox items, pending
	// attachments); nil means everyone.
	AudienceUserID *string   `gorm:"size:36"`
	PayloadJSON    string    `gorm:"column:payload_json;type:text;not null"`
	CreatedAt      time.Time `gorm:"not null;index:ix_change_log_created"`
}

func (ChangeLog) TableName() string { return "change_log" }

type FeedToken struct {
	ID         string `gorm:"primaryKey;size:36"`
	UserID     string `gorm:"size:36;not null;uniqueIndex:ux_feed_tokens_user"`
	TokenHash  string `gorm:"size:64;not null;uniqueIndex:ux_feed_tokens_hash"`
	LastUsedAt *time.Time
	CreatedAt  time.Time `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"not null"`
}

type Setting struct {
	Key       string    `gorm:"primaryKey;size:64"`
	ValueJSON string    `gorm:"column:value_json;type:text;not null"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

// AllModels lists every model, in dependency order, for schema checks and
// export/import.
func AllModels() []any {
	return []any{
		&User{}, &UserPreference{}, &Session{}, &EventType{}, &Event{}, &EventOverride{},
		&Tag{}, &EventTag{}, &Comment{}, &Attachment{}, &Suggestion{},
		&Notification{}, &Activity{}, &ChangeLog{}, &FeedToken{}, &Setting{},
	}
}
