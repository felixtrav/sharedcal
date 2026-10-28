// Package authz holds every SharedCal permission rule as a pure function.
//
// Handlers must call these functions for every request; the UI only mirrors
// the Permissions objects computed here. Nothing in this package touches the
// database or the network, so the rules can be tested exhaustively.
package authz

// Role is a user's single application role.
type Role string

const (
	Viewer Role = "viewer"
	Writer Role = "writer"
	Admin  Role = "admin"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return r == Viewer || r == Writer || r == Admin }

// Rank orders roles so the highest matching group wins during role mapping.
func (r Role) Rank() int {
	switch r {
	case Admin:
		return 3
	case Writer:
		return 2
	case Viewer:
		return 1
	}
	return 0
}

// Actor is the authenticated user making a request.
type Actor struct {
	ID     string
	Role   Role
	Active bool
}

func (a Actor) ok() bool    { return a.ID != "" && a.Active && a.Role.Valid() }
func (a Actor) admin() bool { return a.ok() && a.Role == Admin }
func (a Actor) writer() bool {
	return a.ok() && (a.Role == Writer || a.Role == Admin)
}

// Event is the subset of an event the rules need.
type Event struct {
	OwnerID string
	Deleted bool
}

// Comment is the subset of a comment the rules need.
type Comment struct {
	AuthorID string
	Deleted  bool
}

// Attachment is the subset of an attachment the rules need.
type Attachment struct {
	UploaderID string
	// EventOwnerID is the owner of the event the attachment belongs to.
	EventOwnerID string
	// Pending is true while a virus scan has not yet cleared the file.
	Pending bool
	// Infected files are never served to anyone.
	Infected bool
	Deleted  bool
}

// Suggestion is the subset of a suggestion the rules need.
type Suggestion struct {
	AuthorID     string
	EventOwnerID string
	Open         bool
}

// EditDecision is the outcome of asking whether an actor may edit an event.
type EditDecision int

const (
	// Forbidden: the actor may neither edit nor suggest.
	Forbidden EditDecision = iota
	// SuggestOnly: the actor may propose a suggestion instead.
	SuggestOnly
	// Allowed: the actor may edit directly.
	Allowed
)

func owns(a Actor, ownerID string) bool { return ownerID != "" && a.ID == ownerID }

// CanView: every active signed-in user can view the calendar, events,
// comments, suggestions and (non-pending) attachments.
func CanView(a Actor) bool { return a.ok() }

// CanSubscribeFeed: every active user may have a personal ICS feed.
func CanSubscribeFeed(a Actor) bool { return a.ok() }

// CanCreateEvent: writers and admins.
func CanCreateEvent(a Actor) bool { return a.writer() }

// DecideEdit says whether an actor may edit an event directly, only suggest,
// or neither.
func DecideEdit(a Actor, e Event) EditDecision {
	if !a.writer() || e.Deleted {
		return Forbidden
	}
	if a.admin() || owns(a, e.OwnerID) {
		return Allowed
	}
	return SuggestOnly
}

// CanEditEvent is DecideEdit == Allowed.
func CanEditEvent(a Actor, e Event) bool { return DecideEdit(a, e) == Allowed }

// CanDeleteEvent: owner (writer+) or admin.
func CanDeleteEvent(a Actor, e Event) bool { return CanEditEvent(a, e) }

// CanRestoreEvent: admins restore from trash. Owners additionally get the
// short "Undo" window, which the caller enforces with UndoWindow.
func CanRestoreEvent(a Actor, e Event, withinUndoWindow bool) bool {
	if !e.Deleted || !a.ok() {
		return false
	}
	if a.admin() {
		return true
	}
	return withinUndoWindow && a.writer() && owns(a, e.OwnerID)
}

// CanTransferOwnership: admins only.
func CanTransferOwnership(a Actor) bool { return a.admin() }

// CanManageEventAttachments: add/remove attachments on an event. Writers on
// their own events, admins on any.
func CanManageEventAttachments(a Actor, e Event) bool { return CanEditEvent(a, e) }

// CanSuggest: writers and admins on events they don't own. Admins rarely
// need it but are not prevented.
func CanSuggest(a Actor, e Event) bool {
	if !a.writer() || e.Deleted {
		return false
	}
	return !owns(a, e.OwnerID)
}

// CanReviewSuggestion: accept or reject. Owner of the event (writer+) or admin.
func CanReviewSuggestion(a Actor, s Suggestion) bool {
	if !s.Open || !a.writer() {
		return false
	}
	return a.admin() || owns(a, s.EventOwnerID)
}

// CanWithdrawSuggestion: only the author, only while open.
func CanWithdrawSuggestion(a Actor, s Suggestion) bool {
	return s.Open && a.ok() && owns(a, s.AuthorID)
}

// CanComment: every active user, on any non-deleted event.
func CanComment(a Actor, e Event) bool { return a.ok() && !e.Deleted }

// CanEditComment: the author only (admins may delete but not rewrite).
func CanEditComment(a Actor, c Comment) bool {
	return a.ok() && !c.Deleted && owns(a, c.AuthorID)
}

// CanDeleteComment: the author or an admin.
func CanDeleteComment(a Actor, c Comment) bool {
	if !a.ok() || c.Deleted {
		return false
	}
	return a.admin() || owns(a, c.AuthorID)
}

// CanAttachToComment: writers and admins, and only to their own comments.
func CanAttachToComment(a Actor, c Comment) bool {
	return a.writer() && !c.Deleted && owns(a, c.AuthorID)
}

// CanSeeAttachment: everyone sees clean (or unscanned) files; pending files
// only their uploader; infected files no one.
func CanSeeAttachment(a Actor, at Attachment) bool {
	if !a.ok() || at.Deleted || at.Infected {
		return false
	}
	if at.Pending {
		return owns(a, at.UploaderID)
	}
	return true
}

// CanDeleteAttachment: admin; the event owner (writer+); or the uploader
// while they are still a writer.
func CanDeleteAttachment(a Actor, at Attachment) bool {
	if !a.writer() || at.Deleted {
		return false
	}
	return a.admin() || owns(a, at.EventOwnerID) || owns(a, at.UploaderID)
}

// CanAttachFilesToComments: writers and admins may attach files to the
// comments they write (checked before the comment exists).
func CanAttachFilesToComments(a Actor) bool { return a.writer() }

// CanRestoreAttachment: admins only.
func CanRestoreAttachment(a Actor) bool { return a.admin() }

// CanAdminister: users, roles, tags, themes, settings, global activity, trash.
func CanAdminister(a Actor) bool { return a.admin() }

// CanEraseUser: admins may erase another user's personal data, never their
// own (that would also remove the admin doing it).
func CanEraseUser(a Actor, userID string) bool { return a.admin() && !owns(a, userID) }

// ActivityScope says which audit-log rows an actor may read.
type ActivityScope int

const (
	ActivityNone ActivityScope = iota
	ActivityOwn
	ActivityAll
)

// ActivityAccess: admins see everything; everyone else their own activity.
func ActivityAccess(a Actor) ActivityScope {
	switch {
	case a.admin():
		return ActivityAll
	case a.ok():
		return ActivityOwn
	}
	return ActivityNone
}

// CanViewEventHistory: event history is part of viewing the event.
func CanViewEventHistory(a Actor) bool { return a.ok() }

// Permissions is the per-request object sent with each event so the UI can
// hide controls without re-implementing the rules.
type Permissions struct {
	Edit    bool `json:"edit"`
	Delete  bool `json:"delete"`
	Suggest bool `json:"suggest"`
	Comment bool `json:"comment"`
	Attach  bool `json:"attach"`
	Review  bool `json:"review"`
}

// EventPermissions computes the Permissions object for an actor and event.
func EventPermissions(a Actor, e Event) Permissions {
	return Permissions{
		Edit:    CanEditEvent(a, e),
		Delete:  CanDeleteEvent(a, e),
		Suggest: CanSuggest(a, e),
		Comment: CanComment(a, e),
		Attach:  CanManageEventAttachments(a, e),
		Review:  CanReviewSuggestion(a, Suggestion{EventOwnerID: e.OwnerID, Open: true}),
	}
}

// CommentPermissions is sent with each comment.
type CommentPermissions struct {
	Edit   bool `json:"edit"`
	Delete bool `json:"delete"`
	Attach bool `json:"attach"`
}

// CommentPerms computes CommentPermissions.
func CommentPerms(a Actor, c Comment) CommentPermissions {
	return CommentPermissions{
		Edit:   CanEditComment(a, c),
		Delete: CanDeleteComment(a, c),
		Attach: CanAttachToComment(a, c),
	}
}

// SuggestionPermissions is sent with each suggestion.
type SuggestionPermissions struct {
	Review   bool `json:"review"`
	Withdraw bool `json:"withdraw"`
}

// SuggestionPerms computes SuggestionPermissions.
func SuggestionPerms(a Actor, s Suggestion) SuggestionPermissions {
	return SuggestionPermissions{
		Review:   CanReviewSuggestion(a, s),
		Withdraw: CanWithdrawSuggestion(a, s),
	}
}
