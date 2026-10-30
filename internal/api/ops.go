package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/felixtrav/sharedcal/internal/auth"
	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/domain"
	"github.com/felixtrav/sharedcal/internal/realtime"
)

// register adds an operation with SharedCal defaults (body cap, error mapping).
func register[I, O any](s *Server, op huma.Operation, h func(context.Context, *I) (*O, error)) {
	if op.MaxBodyBytes == 0 {
		op.MaxBodyBytes = maxJSONBody
	}
	if op.Security == nil {
		op.Security = []map[string][]string{{"session": {}}}
	}
	huma.Register(s.api, op, func(ctx context.Context, in *I) (*O, error) {
		out, err := h(ctx, in)
		if err != nil {
			return nil, toProblem(ctx, s.Log, err)
		}
		return out, nil
	})
}

// withActor wraps a handler that needs a signed-in actor.
func withActor[I, O any](h func(context.Context, authz.Actor, *I) (*O, error)) func(context.Context, *I) (*O, error) {
	return func(ctx context.Context, in *I) (*O, error) {
		a, err := actorFrom(ctx)
		if err != nil {
			return nil, err
		}
		return h(ctx, a, in)
	}
}

// Shared I/O shapes.
type (
	idPath struct {
		ID string `path:"id" maxLength:"36"`
	}
	body[T any] struct {
		Body T
	}
	empty struct{}
)

func ok[T any](v T) *body[T] { return &body[T]{Body: v} }

// MeBody is GET /api/me.
type MeBody struct {
	User struct {
		domain.UserRef
		Email string `json:"email"`
		Role  string `json:"role" enum:"viewer,writer,admin"`
	} `json:"user"`
	Preferences domain.Preferences `json:"preferences"`
	CSRFToken   string             `json:"csrfToken"`
	Permissions struct {
		CreateEvent      bool `json:"createEvent"`
		AttachToComments bool `json:"attachToComments"`
		Admin            bool `json:"admin"`
	} `json:"permissions"`
	Instance domain.PublicSettings `json:"instance"`
}

type eventsQuery struct {
	From  time.Time `query:"from" required:"true" doc:"Range start (inclusive), RFC 3339"`
	To    time.Time `query:"to" required:"true" doc:"Range end (exclusive), RFC 3339"`
	Tag   []string  `query:"tag,explode" maxItems:"50"`
	Type  []string  `query:"type,explode" maxItems:"50" doc:"Event type ids"`
	Owner []string  `query:"owner,explode" maxItems:"50"`
	Q     string    `query:"q" maxLength:"200"`
}

type ScopeParams struct {
	ID         string `path:"id" maxLength:"36"`
	IfMatch    string `header:"If-Match" maxLength:"40" doc:"Event version, e.g. 4 or \"4\""`
	Scope      string `query:"scope" enum:"this,following,all" doc:"For recurring events"`
	Occurrence string `query:"occurrence" maxLength:"40" doc:"Original start (RECURRENCE-ID) of the occurrence, RFC 3339"`
}

func (p *ScopeParams) version() (*int64, error) {
	v := strings.TrimSpace(p.IfMatch)
	if v == "" {
		return nil, nil
	}
	v = strings.Trim(strings.TrimPrefix(v, "W/"), `"`)
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 1 {
		return nil, newProblem(http.StatusBadRequest, "If-Match must be the event version")
	}
	return &n, nil
}

func (p *ScopeParams) occurrence() (*time.Time, error) {
	if p.Occurrence == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, p.Occurrence)
	if err != nil {
		return nil, newProblem(http.StatusUnprocessableEntity, "occurrence must be an RFC 3339 instant")
	}
	return &t, nil
}

type patchEventIn struct {
	ScopeParams
	Body domain.EventInput
}

type eventsBody struct {
	Events []domain.EventDTO `json:"events"`
}

func (s *Server) registerOperations() {
	s.api.OpenAPI().Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"session": {Type: "apiKey", In: "cookie", Name: "sharedcal_session", Description: "Session cookie (prefixed __Host- over HTTPS)."},
	}
	s.registerMe()
	s.registerEvents()
	s.registerCollaboration()
	s.registerAdmin()
}

func (s *Server) registerMe() {
	d := s.Domain
	register(s, huma.Operation{OperationID: "getMe", Method: http.MethodGet, Path: "/api/me", Tags: []string{"me"}, Summary: "Current user, preferences and CSRF token"},
		withActor(func(ctx context.Context, a authz.Actor, _ *empty) (*body[MeBody], error) {
			u, prefs, err := d.Me(ctx, a)
			if err != nil {
				return nil, err
			}
			inst, err := d.Public(ctx)
			if err != nil {
				return nil, err
			}
			var b MeBody
			b.User.UserRef = domain.UserRef{ID: u.ID, Name: u.DisplayName, AvatarURL: u.AvatarURL}
			if b.User.Name == "" {
				b.User.Name = u.Email
			}
			b.User.Email, b.User.Role = u.Email, u.Role
			b.Preferences = prefs
			b.CSRFToken = auth.FromContext(ctx).CSRF
			b.Permissions.CreateEvent = authz.CanCreateEvent(a)
			b.Permissions.AttachToComments = authz.CanAttachFilesToComments(a)
			b.Permissions.Admin = authz.CanAdminister(a)
			b.Instance = inst
			return ok(b), nil
		}))
	register(s, huma.Operation{OperationID: "putPreferences", Method: http.MethodPut, Path: "/api/me/preferences", Tags: []string{"me"}, Summary: "Update preferences"},
		withActor(func(ctx context.Context, a authz.Actor, in *body[domain.Preferences]) (*body[domain.Preferences], error) {
			p, err := d.SavePreferences(ctx, a, in.Body)
			return ok(p), err
		}))
	register(s, huma.Operation{OperationID: "getFeedToken", Method: http.MethodGet, Path: "/api/me/feed-token", Tags: []string{"me"}, Summary: "Personal ICS feed status"},
		withActor(func(ctx context.Context, a authz.Actor, _ *empty) (*body[domain.FeedStatus], error) {
			st, err := d.FeedToken(ctx, a)
			return ok(st), err
		}))
	type feedURL struct {
		URL string `json:"url"`
	}
	register(s, huma.Operation{OperationID: "rotateFeedToken", Method: http.MethodPost, Path: "/api/me/feed-token", Tags: []string{"me"}, Summary: "Create or rotate the personal ICS feed URL (shown once)"},
		withActor(func(ctx context.Context, a authz.Actor, _ *empty) (*body[feedURL], error) {
			u, err := d.RotateFeedToken(ctx, a)
			return ok(feedURL{URL: u}), err
		}))
	register(s, huma.Operation{OperationID: "revokeFeedToken", Method: http.MethodDelete, Path: "/api/me/feed-token", Tags: []string{"me"}, Summary: "Revoke the personal ICS feed", DefaultStatus: http.StatusNoContent},
		withActor(func(ctx context.Context, a authz.Actor, _ *empty) (*empty, error) {
			return &empty{}, d.RevokeFeedToken(ctx, a)
		}))
}

func (s *Server) registerEvents() {
	d := s.Domain
	tags := []string{"events"}
	register(s, huma.Operation{OperationID: "listEvents", Method: http.MethodGet, Path: "/api/events", Tags: tags, Summary: "Occurrences in a range, recurrences expanded"},
		withActor(func(ctx context.Context, a authz.Actor, in *eventsQuery) (*body[eventsBody], error) {
			evs, err := d.ListEvents(ctx, a, domain.ListQuery{From: in.From, To: in.To, Tags: in.Tag, Types: in.Type, Owners: in.Owner, Q: in.Q})
			return ok(eventsBody{Events: evs}), err
		}))
	register(s, huma.Operation{OperationID: "getEvent", Method: http.MethodGet, Path: "/api/events/{id}", Tags: tags, Summary: "One event with overrides and attachments"},
		withActor(func(ctx context.Context, a authz.Actor, in *idPath) (*body[domain.EventDetail], error) {
			e, err := d.GetEvent(ctx, a, in.ID)
			if err != nil {
				return nil, err
			}
			return ok(*e), nil
		}))
	register(s, huma.Operation{OperationID: "createEvent", Method: http.MethodPost, Path: "/api/events", Tags: tags, Summary: "Create an event", DefaultStatus: http.StatusCreated},
		withActor(func(ctx context.Context, a authz.Actor, in *body[domain.EventInput]) (*body[domain.EventDTO], error) {
			e, err := d.CreateEvent(ctx, a, in.Body)
			if err != nil {
				return nil, err
			}
			return ok(*e), nil
		}))
	register(s, huma.Operation{OperationID: "updateEvent", Method: http.MethodPatch, Path: "/api/events/{id}", Tags: tags, Summary: "Update an event (requires If-Match)"},
		withActor(func(ctx context.Context, a authz.Actor, in *patchEventIn) (*body[domain.EventDTO], error) {
			v, err := in.version()
			if err != nil {
				return nil, err
			}
			occ, err := in.occurrence()
			if err != nil {
				return nil, err
			}
			e, err := d.UpdateEvent(ctx, a, in.ID, v, in.Scope, occ, in.Body)
			if err != nil {
				return nil, err
			}
			return ok(*e), nil
		}))
	register(s, huma.Operation{OperationID: "deleteEvent", Method: http.MethodDelete, Path: "/api/events/{id}", Tags: tags, Summary: "Soft-delete an event or occurrences", DefaultStatus: http.StatusNoContent},
		withActor(func(ctx context.Context, a authz.Actor, in *ScopeParams) (*empty, error) {
			v, err := in.version()
			if err != nil {
				return nil, err
			}
			occ, err := in.occurrence()
			if err != nil {
				return nil, err
			}
			return &empty{}, d.DeleteEvent(ctx, a, in.ID, v, in.Scope, occ)
		}))
	register(s, huma.Operation{OperationID: "restoreEvent", Method: http.MethodPost, Path: "/api/events/{id}/restore", Tags: tags, Summary: "Restore from trash"},
		withActor(func(ctx context.Context, a authz.Actor, in *idPath) (*body[domain.EventDTO], error) {
			e, err := d.RestoreEvent(ctx, a, in.ID)
			if err != nil {
				return nil, err
			}
			return ok(*e), nil
		}))
	register(s, huma.Operation{OperationID: "watchEvent", Method: http.MethodPut, Path: "/api/events/{id}/watch", Tags: tags, Summary: "Get inbox items when the event changes", DefaultStatus: http.StatusNoContent},
		withActor(func(ctx context.Context, a authz.Actor, in *idPath) (*empty, error) {
			return &empty{}, d.Watch(ctx, a, in.ID)
		}))
	register(s, huma.Operation{OperationID: "unwatchEvent", Method: http.MethodDelete, Path: "/api/events/{id}/watch", Tags: tags, Summary: "Stop getting inbox items for the event", DefaultStatus: http.StatusNoContent},
		withActor(func(ctx context.Context, a authz.Actor, in *idPath) (*empty, error) {
			return &empty{}, d.Unwatch(ctx, a, in.ID)
		}))
	register(s, huma.Operation{OperationID: "listTrash", Method: http.MethodGet, Path: "/api/trash", Tags: tags, Summary: "Deleted events (admins)"},
		withActor(func(ctx context.Context, a authz.Actor, _ *empty) (*body[eventsBody], error) {
			evs, err := d.Trash(ctx, a)
			return ok(eventsBody{Events: evs}), err
		}))
	type attachmentTrashBody struct {
		Attachments []domain.TrashedAttachment `json:"attachments"`
	}
	register(s, huma.Operation{OperationID: "listAttachmentTrash", Method: http.MethodGet, Path: "/api/trash/attachments", Tags: []string{"attachments"}, Summary: "Removed attachments that can still be restored (admins)"},
		withActor(func(ctx context.Context, a authz.Actor, _ *empty) (*body[attachmentTrashBody], error) {
			list, err := d.AttachmentTrash(ctx, a)
			return ok(attachmentTrashBody{Attachments: list}), err
		}))
	type historyBody struct {
		Items []domain.ActivityDTO `json:"items"`
	}
	register(s, huma.Operation{OperationID: "eventHistory", Method: http.MethodGet, Path: "/api/events/{id}/history", Tags: tags, Summary: "Activity for one event"},
		withActor(func(ctx context.Context, a authz.Actor, in *idPath) (*body[historyBody], error) {
			items, err := d.EventHistory(ctx, a, in.ID)
			return ok(historyBody{Items: items}), err
		}))
}

func (s *Server) registerCollaboration() {
	d := s.Domain
	type commentsBody struct {
		Comments []domain.CommentDTO `json:"comments"`
	}
	type commentIn struct {
		ID   string `path:"id" maxLength:"36"`
		Body domain.CommentInput
	}
	ct := []string{"comments"}
	register(s, huma.Operation{OperationID: "listComments", Method: http.MethodGet, Path: "/api/events/{id}/comments", Tags: ct, Summary: "Comments on an event"},
		withActor(func(ctx context.Context, a authz.Actor, in *idPath) (*body[commentsBody], error) {
			cs, err := d.ListComments(ctx, a, in.ID)
			return ok(commentsBody{Comments: cs}), err
		}))
	register(s, huma.Operation{OperationID: "createComment", Method: http.MethodPost, Path: "/api/events/{id}/comments", Tags: ct, Summary: "Add a comment or reply", DefaultStatus: http.StatusCreated},
		withActor(func(ctx context.Context, a authz.Actor, in *commentIn) (*body[domain.CommentDTO], error) {
			c, err := d.CreateComment(ctx, a, in.ID, in.Body)
			if err != nil {
				return nil, err
			}
			return ok(*c), nil
		}))
	register(s, huma.Operation{OperationID: "updateComment", Method: http.MethodPatch, Path: "/api/comments/{id}", Tags: ct, Summary: "Edit own comment"},
		withActor(func(ctx context.Context, a authz.Actor, in *commentIn) (*body[domain.CommentDTO], error) {
			c, err := d.UpdateComment(ctx, a, in.ID, in.Body)
			if err != nil {
				return nil, err
			}
			return ok(*c), nil
		}))
	register(s, huma.Operation{OperationID: "deleteComment", Method: http.MethodDelete, Path: "/api/comments/{id}", Tags: ct, Summary: "Delete a comment", DefaultStatus: http.StatusNoContent},
		withActor(func(ctx context.Context, a authz.Actor, in *idPath) (*empty, error) {
			return &empty{}, d.DeleteComment(ctx, a, in.ID)
		}))

	st := []string{"suggestions"}
	type suggestionsBody struct {
		Suggestions []domain.SuggestionDTO `json:"suggestions"`
	}
	type listSuggIn struct {
		ID     string `path:"id" maxLength:"36"`
		Status string `query:"status" enum:"open,all" default:"open"`
	}
	register(s, huma.Operation{OperationID: "listSuggestions", Method: http.MethodGet, Path: "/api/events/{id}/suggestions", Tags: st, Summary: "Suggestions on an event"},
		withActor(func(ctx context.Context, a authz.Actor, in *listSuggIn) (*body[suggestionsBody], error) {
			list, err := d.ListSuggestions(ctx, a, in.ID, in.Status == "all")
			return ok(suggestionsBody{Suggestions: list}), err
		}))
	type createSuggIn struct {
		ID   string `path:"id" maxLength:"36"`
		Body domain.SuggestionInput
	}
	register(s, huma.Operation{OperationID: "createSuggestion", Method: http.MethodPost, Path: "/api/events/{id}/suggestions", Tags: st, Summary: "Propose an edit or deletion", DefaultStatus: http.StatusCreated},
		withActor(func(ctx context.Context, a authz.Actor, in *createSuggIn) (*body[domain.SuggestionDTO], error) {
			sg, err := d.CreateSuggestion(ctx, a, in.ID, in.Body)
			if err != nil {
				return nil, err
			}
			return ok(*sg), nil
		}))
	type acceptIn struct {
		ID   string `path:"id" maxLength:"36"`
		Body domain.AcceptInput
	}
	register(s, huma.Operation{OperationID: "acceptSuggestion", Method: http.MethodPost, Path: "/api/suggestions/{id}/accept", Tags: st, Summary: "Accept all or selected fields"},
		withActor(func(ctx context.Context, a authz.Actor, in *acceptIn) (*body[domain.AcceptResult], error) {
			r, err := d.AcceptSuggestion(ctx, a, in.ID, in.Body)
			if err != nil {
				return nil, err
			}
			return ok(*r), nil
		}))
	type rejectIn struct {
		ID   string `path:"id" maxLength:"36"`
		Body struct {
			Note string `json:"note,omitempty" maxLength:"2000"`
		}
	}
	register(s, huma.Operation{OperationID: "rejectSuggestion", Method: http.MethodPost, Path: "/api/suggestions/{id}/reject", Tags: st, Summary: "Reject with an optional note"},
		withActor(func(ctx context.Context, a authz.Actor, in *rejectIn) (*body[domain.SuggestionDTO], error) {
			sg, err := d.RejectSuggestion(ctx, a, in.ID, in.Body.Note)
			if err != nil {
				return nil, err
			}
			return ok(*sg), nil
		}))
	register(s, huma.Operation{OperationID: "withdrawSuggestion", Method: http.MethodPost, Path: "/api/suggestions/{id}/withdraw", Tags: st, Summary: "Withdraw own suggestion"},
		withActor(func(ctx context.Context, a authz.Actor, in *idPath) (*body[domain.SuggestionDTO], error) {
			sg, err := d.WithdrawSuggestion(ctx, a, in.ID)
			if err != nil {
				return nil, err
			}
			return ok(*sg), nil
		}))

	at := []string{"attachments"}
	register(s, huma.Operation{OperationID: "deleteAttachment", Method: http.MethodDelete, Path: "/api/attachments/{id}", Tags: at, Summary: "Remove an attachment", DefaultStatus: http.StatusNoContent},
		withActor(func(ctx context.Context, a authz.Actor, in *idPath) (*empty, error) {
			return &empty{}, d.DeleteAttachment(ctx, a, in.ID)
		}))
	register(s, huma.Operation{OperationID: "restoreAttachment", Method: http.MethodPost, Path: "/api/attachments/{id}/restore", Tags: at, Summary: "Restore a removed attachment (admins)"},
		withActor(func(ctx context.Context, a authz.Actor, in *idPath) (*body[domain.AttachmentDTO], error) {
			att, err := d.RestoreAttachment(ctx, a, in.ID)
			if err != nil {
				return nil, err
			}
			return ok(*att), nil
		}))

	it := []string{"inbox"}
	type inboxIn struct {
		Unread bool   `query:"unread"`
		Before string `query:"before" maxLength:"36"`
		Limit  int    `query:"limit" minimum:"1" maximum:"200" default:"50"`
	}
	type inboxBody struct {
		Items  []domain.NotificationDTO `json:"items"`
		Unread int64                    `json:"unread"`
	}
	register(s, huma.Operation{OperationID: "listInbox", Method: http.MethodGet, Path: "/api/inbox", Tags: it, Summary: "Inbox items, newest first"},
		withActor(func(ctx context.Context, a authz.Actor, in *inboxIn) (*body[inboxBody], error) {
			items, unread, err := d.Inbox(ctx, a, in.Unread, in.Before, in.Limit)
			return ok(inboxBody{Items: items, Unread: unread}), err
		}))
	type readIn struct {
		Body struct {
			IDs []string `json:"ids,omitempty" maxItems:"500"`
			All bool     `json:"all,omitempty"`
		}
	}
	type unreadBody struct {
		Unread int64 `json:"unread"`
	}
	register(s, huma.Operation{OperationID: "markInboxRead", Method: http.MethodPost, Path: "/api/inbox/read", Tags: it, Summary: "Mark items read"},
		withActor(func(ctx context.Context, a authz.Actor, in *readIn) (*body[unreadBody], error) {
			n, err := d.MarkRead(ctx, a, in.Body.IDs, in.Body.All)
			return ok(unreadBody{Unread: n}), err
		}))

	type presenceIn struct {
		Body struct {
			EventID string `json:"eventId" maxLength:"36"`
			State   string `json:"state" enum:"viewing,editing,left"`
		}
	}
	register(s, huma.Operation{OperationID: "presence", Method: http.MethodPost, Path: "/api/presence", Tags: []string{"realtime"}, Summary: "Viewing/editing heartbeat", DefaultStatus: http.StatusNoContent, MaxBodyBytes: 1024},
		withActor(func(ctx context.Context, a authz.Actor, in *presenceIn) (*empty, error) {
			if !authz.CanView(a) {
				return nil, newProblem(http.StatusForbidden, "not allowed")
			}
			if in.Body.State != realtime.PresenceLeft && !d.EventExists(ctx, in.Body.EventID) {
				return nil, newProblem(http.StatusNotFound, "event not found")
			}
			u := auth.FromContext(ctx).User
			name := u.DisplayName
			if name == "" {
				name = u.Email
			}
			s.Presence.Update(in.Body.EventID, realtime.Viewer{ID: u.ID, Name: name, AvatarURL: u.AvatarURL, State: in.Body.State})
			return &empty{}, nil
		}))
}

func (s *Server) registerAdmin() {
	d := s.Domain
	tt := []string{"tags"}
	type tagsBody struct {
		Tags []domain.TagDTO `json:"tags"`
	}
	type tagIn struct {
		ID   string `path:"id" maxLength:"36"`
		Body domain.TagInput
	}
	register(s, huma.Operation{OperationID: "listTags", Method: http.MethodGet, Path: "/api/tags", Tags: tt, Summary: "All tags"},
		withActor(func(ctx context.Context, a authz.Actor, _ *empty) (*body[tagsBody], error) {
			t, err := d.ListTags(ctx, a)
			return ok(tagsBody{Tags: t}), err
		}))
	register(s, huma.Operation{OperationID: "createTag", Method: http.MethodPost, Path: "/api/tags", Tags: tt, Summary: "Create a tag", DefaultStatus: http.StatusCreated},
		withActor(func(ctx context.Context, a authz.Actor, in *body[domain.TagInput]) (*body[domain.TagDTO], error) {
			t, err := d.CreateTag(ctx, a, in.Body)
			if err != nil {
				return nil, err
			}
			return ok(*t), nil
		}))
	register(s, huma.Operation{OperationID: "updateTag", Method: http.MethodPatch, Path: "/api/tags/{id}", Tags: tt, Summary: "Rename or recolor"},
		withActor(func(ctx context.Context, a authz.Actor, in *tagIn) (*body[domain.TagDTO], error) {
			t, err := d.UpdateTag(ctx, a, in.ID, in.Body)
			if err != nil {
				return nil, err
			}
			return ok(*t), nil
		}))
	register(s, huma.Operation{OperationID: "deleteTag", Method: http.MethodDelete, Path: "/api/tags/{id}", Tags: tt, Summary: "Delete a tag", DefaultStatus: http.StatusNoContent},
		withActor(func(ctx context.Context, a authz.Actor, in *idPath) (*empty, error) {
			return &empty{}, d.DeleteTag(ctx, a, in.ID)
		}))
	type reorderIn struct {
		Body struct {
			IDs []string `json:"ids" maxItems:"200"`
		}
	}
	register(s, huma.Operation{OperationID: "reorderTags", Method: http.MethodPost, Path: "/api/tags/reorder", Tags: tt, Summary: "Set tag order"},
		withActor(func(ctx context.Context, a authz.Actor, in *reorderIn) (*body[tagsBody], error) {
			t, err := d.ReorderTags(ctx, a, in.Body.IDs)
			return ok(tagsBody{Tags: t}), err
		}))

	et := []string{"event types"}
	type eventTypesBody struct {
		EventTypes []domain.EventTypeDTO `json:"eventTypes"`
	}
	type eventTypeIn struct {
		ID   string `path:"id" maxLength:"36"`
		Body domain.EventTypeInput
	}
	register(s, huma.Operation{OperationID: "listEventTypes", Method: http.MethodGet, Path: "/api/event-types", Tags: et, Summary: "All event types"},
		withActor(func(ctx context.Context, a authz.Actor, _ *empty) (*body[eventTypesBody], error) {
			t, err := d.ListEventTypes(ctx, a)
			return ok(eventTypesBody{EventTypes: t}), err
		}))
	register(s, huma.Operation{OperationID: "createEventType", Method: http.MethodPost, Path: "/api/event-types", Tags: et, Summary: "Create an event type", DefaultStatus: http.StatusCreated},
		withActor(func(ctx context.Context, a authz.Actor, in *body[domain.EventTypeInput]) (*body[domain.EventTypeDTO], error) {
			t, err := d.CreateEventType(ctx, a, in.Body)
			if err != nil {
				return nil, err
			}
			return ok(*t), nil
		}))
	register(s, huma.Operation{OperationID: "updateEventType", Method: http.MethodPatch, Path: "/api/event-types/{id}", Tags: et, Summary: "Change an event type's name, look, defaults or fields"},
		withActor(func(ctx context.Context, a authz.Actor, in *eventTypeIn) (*body[domain.EventTypeDTO], error) {
			t, err := d.UpdateEventType(ctx, a, in.ID, in.Body)
			if err != nil {
				return nil, err
			}
			return ok(*t), nil
		}))
	register(s, huma.Operation{OperationID: "deleteEventType", Method: http.MethodDelete, Path: "/api/event-types/{id}", Tags: et, Summary: "Delete an unused event type", DefaultStatus: http.StatusNoContent},
		withActor(func(ctx context.Context, a authz.Actor, in *idPath) (*empty, error) {
			return &empty{}, d.DeleteEventType(ctx, a, in.ID)
		}))
	register(s, huma.Operation{OperationID: "reorderEventTypes", Method: http.MethodPost, Path: "/api/event-types/reorder", Tags: et, Summary: "Set event type order"},
		withActor(func(ctx context.Context, a authz.Actor, in *reorderIn) (*body[eventTypesBody], error) {
			t, err := d.ReorderEventTypes(ctx, a, in.Body.IDs)
			return ok(eventTypesBody{EventTypes: t}), err
		}))

	register(s, huma.Operation{OperationID: "importIcs", Method: http.MethodPost, Path: "/api/import/ics", Tags: []string{"import"}, Summary: "Import events from an iCalendar file, or preview the import (admins)", MaxBodyBytes: 2_500_000},
		withActor(func(ctx context.Context, a authz.Actor, in *body[domain.ICSImportInput]) (*body[domain.ICSImportResult], error) {
			r, err := d.ImportICS(ctx, a, in.Body)
			if err != nil {
				return nil, err
			}
			return ok(*r), nil
		}))

	ut := []string{"users"}
	type directoryBody struct {
		Users []domain.UserRef `json:"users"`
	}
	register(s, huma.Operation{OperationID: "userDirectory", Method: http.MethodGet, Path: "/api/users/directory", Tags: ut, Summary: "Active users (for mentions and filters)"},
		withActor(func(ctx context.Context, a authz.Actor, _ *empty) (*body[directoryBody], error) {
			u, err := d.Directory(ctx, a)
			return ok(directoryBody{Users: u}), err
		}))
	type usersBody struct {
		Users []domain.AdminUser `json:"users"`
	}
	register(s, huma.Operation{OperationID: "listUsers", Method: http.MethodGet, Path: "/api/users", Tags: ut, Summary: "All users (admins)"},
		withActor(func(ctx context.Context, a authz.Actor, _ *empty) (*body[usersBody], error) {
			u, err := d.ListUsers(ctx, a)
			return ok(usersBody{Users: u}), err
		}))
	type userPatchIn struct {
		ID   string `path:"id" maxLength:"36"`
		Body domain.UserPatch
	}
	register(s, huma.Operation{OperationID: "updateUser", Method: http.MethodPatch, Path: "/api/users/{id}", Tags: ut, Summary: "Change role, role source or active"},
		withActor(func(ctx context.Context, a authz.Actor, in *userPatchIn) (*body[domain.AdminUser], error) {
			u, err := d.UpdateUser(ctx, a, in.ID, in.Body)
			if err != nil {
				return nil, err
			}
			return ok(*u), nil
		}))
	type eraseIn struct {
		ID         string `path:"id" maxLength:"36"`
		TransferTo string `query:"transferTo" maxLength:"36" doc:"Required when the user owns live events: who receives them"`
	}
	register(s, huma.Operation{OperationID: "eraseUser", Method: http.MethodDelete, Path: "/api/users/{id}", Tags: ut, Summary: "Erase a user's personal data (admins)"},
		withActor(func(ctx context.Context, a authz.Actor, in *eraseIn) (*body[domain.EraseResult], error) {
			r, err := d.EraseUser(ctx, a, in.ID, in.TransferTo)
			if err != nil {
				return nil, err
			}
			return ok(*r), nil
		}))
	type sessionsBody struct {
		Sessions []domain.SessionInfo `json:"sessions"`
	}
	register(s, huma.Operation{OperationID: "listSessions", Method: http.MethodGet, Path: "/api/users/{id}/sessions", Tags: ut, Summary: "A user's sessions"},
		withActor(func(ctx context.Context, a authz.Actor, in *idPath) (*body[sessionsBody], error) {
			list, err := d.ListSessions(ctx, a, in.ID, auth.FromContext(ctx).Session.ID)
			return ok(sessionsBody{Sessions: list}), err
		}))
	type revokeIn struct {
		ID        string `path:"id" maxLength:"36"`
		SessionID string `query:"id" maxLength:"16"`
	}
	register(s, huma.Operation{OperationID: "revokeSessions", Method: http.MethodDelete, Path: "/api/users/{id}/sessions", Tags: ut, Summary: "Revoke one or all sessions", DefaultStatus: http.StatusNoContent},
		withActor(func(ctx context.Context, a authz.Actor, in *revokeIn) (*empty, error) {
			return &empty{}, d.RevokeSessions(ctx, a, in.ID, in.SessionID)
		}))
	type transferIn struct {
		ID   string `path:"id" maxLength:"36"`
		Body struct {
			ToUserID string `json:"toUserId" maxLength:"36"`
		}
	}
	type movedBody struct {
		Moved int64 `json:"moved"`
	}
	register(s, huma.Operation{OperationID: "transferOwnership", Method: http.MethodPost, Path: "/api/users/{id}/transfer", Tags: ut, Summary: "Move all of a user's events to another user"},
		withActor(func(ctx context.Context, a authz.Actor, in *transferIn) (*body[movedBody], error) {
			n, err := d.TransferOwnership(ctx, a, in.ID, in.Body.ToUserID)
			return ok(movedBody{Moved: n}), err
		}))

	type activityIn struct {
		Actor       string `query:"actor" maxLength:"36"`
		Action      string `query:"action" maxLength:"64"`
		SubjectType string `query:"subjectType" maxLength:"32"`
		EventID     string `query:"eventId" maxLength:"36"`
		Before      string `query:"before" maxLength:"36"`
		Limit       int    `query:"limit" minimum:"1" maximum:"200" default:"50"`
	}
	type activityBody struct {
		Items []domain.ActivityDTO `json:"items"`
	}
	register(s, huma.Operation{OperationID: "listActivity", Method: http.MethodGet, Path: "/api/activity", Tags: []string{"activity"}, Summary: "Audit log (own activity unless admin)"},
		withActor(func(ctx context.Context, a authz.Actor, in *activityIn) (*body[activityBody], error) {
			items, err := d.ListActivity(ctx, a, domain.ActivityFilter{ActorID: in.Actor, Action: in.Action, SubjectType: in.SubjectType, EventID: in.EventID, Before: in.Before, Limit: in.Limit})
			return ok(activityBody{Items: items}), err
		}))

	sett := []string{"settings"}
	register(s, huma.Operation{OperationID: "getSettings", Method: http.MethodGet, Path: "/api/settings", Tags: sett, Summary: "Instance settings (admins)"},
		withActor(func(ctx context.Context, a authz.Actor, _ *empty) (*body[domain.Settings], error) {
			st, err := d.GetSettings(ctx, a)
			if err != nil {
				return nil, err
			}
			return ok(*st), nil
		}))
	register(s, huma.Operation{OperationID: "putSettings", Method: http.MethodPut, Path: "/api/settings", Tags: sett, Summary: "Update instance settings"},
		withActor(func(ctx context.Context, a authz.Actor, in *body[domain.Settings]) (*body[domain.Settings], error) {
			st, err := d.UpdateSettings(ctx, a, in.Body)
			if err != nil {
				return nil, err
			}
			return ok(*st), nil
		}))
	register(s, huma.Operation{OperationID: "deleteLogo", Method: http.MethodDelete, Path: "/api/settings/logo", Tags: sett, Summary: "Remove the logo", DefaultStatus: http.StatusNoContent},
		withActor(func(ctx context.Context, a authz.Actor, _ *empty) (*empty, error) {
			return &empty{}, d.RemoveLogo(ctx, a)
		}))

	tht := []string{"themes"}
	type themesIn struct {
		All bool `query:"all"`
	}
	type themesBody struct {
		Themes []domain.Theme `json:"themes"`
	}
	register(s, huma.Operation{OperationID: "listThemes", Method: http.MethodGet, Path: "/api/themes", Tags: tht, Summary: "Enabled themes (all installed for admins with all=true)"},
		withActor(func(ctx context.Context, a authz.Actor, in *themesIn) (*body[themesBody], error) {
			t, err := d.ListThemes(ctx, a, in.All)
			return ok(themesBody{Themes: t}), err
		}))
	register(s, huma.Operation{OperationID: "installTheme", Method: http.MethodPost, Path: "/api/themes", Tags: tht, Summary: "Upload a theme", DefaultStatus: http.StatusCreated, MaxBodyBytes: 64 << 10},
		withActor(func(ctx context.Context, a authz.Actor, in *body[domain.Theme]) (*body[domain.Theme], error) {
			t, err := d.InstallTheme(ctx, a, in.Body)
			if err != nil {
				return nil, err
			}
			return ok(*t), nil
		}))
	type themeIDIn struct {
		ID string `path:"id" maxLength:"64"`
	}
	register(s, huma.Operation{OperationID: "removeTheme", Method: http.MethodDelete, Path: "/api/themes/{id}", Tags: tht, Summary: "Remove an uploaded theme", DefaultStatus: http.StatusNoContent},
		withActor(func(ctx context.Context, a authz.Actor, in *themeIDIn) (*empty, error) {
			return &empty{}, d.RemoveTheme(ctx, a, in.ID)
		}))
}
