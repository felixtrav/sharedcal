package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/felixtrav/sharedcal/internal/attachments"
	"github.com/felixtrav/sharedcal/internal/auth"
	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/domain"
	"github.com/felixtrav/sharedcal/internal/ics"
	"github.com/felixtrav/sharedcal/internal/realtime"
	"github.com/felixtrav/sharedcal/internal/store"
)

// multipartOverhead is headroom for multipart framing on top of the file.
const multipartOverhead = 64 << 10

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// --- SSE -------------------------------------------------------------------

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if p == nil {
		writeProblem(w, errUnauthenticated())
		return
	}
	err := s.stream.Serve(w, r, realtime.Conn{UserID: p.User.ID, SessionHash: p.Session.ID})
	if errors.Is(err, realtime.ErrTooManyStreams) {
		writeProblem(w, newProblem(http.StatusTooManyRequests, "too many open streams"))
	} else if err != nil {
		s.Log.ErrorContext(r.Context(), "stream failed", "err", err)
	}
}

// actorCache resolves a user's current role for personalizing SSE payloads,
// so a role change is reflected within the TTL (and immediately on
// user.role_changed).
type actorCache struct {
	db  *store.DB
	ttl time.Duration
	mu  sync.Mutex
	m   map[string]cachedActor
}

type cachedActor struct {
	a   authz.Actor
	exp time.Time
}

func newActorCache(db *store.DB, ttl time.Duration) *actorCache {
	return &actorCache{db: db, ttl: ttl, m: map[string]cachedActor{}}
}

func (c *actorCache) get(ctx context.Context, userID string) authz.Actor {
	c.mu.Lock()
	if e, ok := c.m[userID]; ok && time.Now().Before(e.exp) {
		c.mu.Unlock()
		return e.a
	}
	c.mu.Unlock()
	var u store.User
	a := authz.Actor{}
	if err := c.db.R.WithContext(ctx).Where("id = ?", userID).Take(&u).Error; err == nil {
		a = authz.Actor{ID: u.ID, Role: authz.Role(u.Role), Active: u.Active}
	}
	c.mu.Lock()
	if len(c.m) > 10000 {
		c.m = map[string]cachedActor{}
	}
	c.m[userID] = cachedActor{a: a, exp: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return a
}

func (c *actorCache) forget(userID string) {
	c.mu.Lock()
	delete(c.m, userID)
	c.mu.Unlock()
}

// personalize recomputes the permissions part of a payload for one
// recipient, since the broker fans the same message out to everyone.
func (s *Server) personalize(ctx context.Context, m realtime.Message, c realtime.Conn) (json.RawMessage, bool) {
	if m.Type == "user.role_changed" {
		var p struct {
			UserID string `json:"userId"`
		}
		if json.Unmarshal(m.Payload, &p) == nil {
			s.actors.forget(p.UserID)
		}
		return m.Payload, true
	}
	actor := s.actors.get(ctx, c.UserID)
	if !actor.Active {
		return nil, false
	}
	switch m.Type {
	case "event.created", "event.updated":
		var e domain.EventDTO
		if json.Unmarshal(m.Payload, &e) != nil {
			return nil, false
		}
		e.Permissions = authz.EventPermissions(actor, authz.Event{OwnerID: e.Owner.ID, Deleted: e.DeletedAt != nil})
		return marshal(e)
	case "comment.created", "comment.updated":
		var cm domain.CommentDTO
		if json.Unmarshal(m.Payload, &cm) != nil {
			return nil, false
		}
		cm.Permissions = authz.CommentPerms(actor, authz.Comment{AuthorID: cm.Author.ID})
		for i := range cm.Attachments {
			domain.AttachmentPermissions(actor, &cm.Attachments[i])
		}
		return marshal(cm)
	case "attachment.added", "attachment.scanned":
		var a domain.AttachmentDTO
		if json.Unmarshal(m.Payload, &a) != nil {
			return nil, false
		}
		domain.AttachmentPermissions(actor, &a)
		return marshal(a)
	case "suggestion.created", "suggestion.resolved":
		var sg domain.SuggestionDTO
		if json.Unmarshal(m.Payload, &sg) != nil {
			return nil, false
		}
		sg.Permissions = authz.SuggestionPerms(actor, authz.Suggestion{AuthorID: sg.Author.ID, EventOwnerID: sg.EventOwnerID, Open: sg.Status == store.SuggestionOpen})
		return marshal(sg)
	case "presence":
		// Nobody needs to be told they are viewing an event themselves.
		var p struct {
			EventID string            `json:"eventId"`
			Viewers []realtime.Viewer `json:"viewers"`
		}
		if json.Unmarshal(m.Payload, &p) != nil {
			return nil, false
		}
		others := p.Viewers[:0]
		for _, v := range p.Viewers {
			if v.ID != actor.ID {
				others = append(others, v)
			}
		}
		p.Viewers = others
		return marshal(p)
	}
	return m.Payload, true
}

func marshal(v any) (json.RawMessage, bool) {
	b, err := json.Marshal(v)
	return b, err == nil
}

// --- uploads ---------------------------------------------------------------

// readFilePart returns the first multipart part named "file", streaming it
// without buffering the body.
func readFilePart(w http.ResponseWriter, r *http.Request, max int64) (string, io.Reader, error) {
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || ct != "multipart/form-data" {
		return "", nil, newProblem(http.StatusUnsupportedMediaType, "send the file as multipart/form-data")
	}
	r.Body = http.MaxBytesReader(w, r.Body, max+multipartOverhead)
	mr, err := r.MultipartReader()
	if err != nil {
		return "", nil, newProblem(http.StatusBadRequest, "malformed multipart body")
	}
	for i := 0; i < 5; i++ {
		part, err := mr.NextPart()
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				return "", nil, err
			}
			return "", nil, newProblem(http.StatusBadRequest, "missing file part")
		}
		if part.FormName() == "file" {
			// FileName() already strips directories; the pipeline sanitizes further.
			return part.FileName(), part, nil
		}
		_ = part.Close()
	}
	return "", nil, newProblem(http.StatusBadRequest, "missing file part")
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request, do func(ctx context.Context, a authz.Actor, up domain.Upload) (any, error)) {
	a, err := actorFrom(r.Context())
	if err != nil {
		writeProblem(w, toProblem(r.Context(), s.Log, err))
		return
	}
	name, body, err := readFilePart(w, r, s.MaxUpload)
	if err != nil {
		writeProblem(w, toProblem(r.Context(), s.Log, err))
		return
	}
	out, err := do(r.Context(), a, domain.Upload{Filename: name, Body: body})
	if err != nil {
		writeProblem(w, toProblem(r.Context(), s.Log, err))
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) uploadEventAttachment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.upload(w, r, func(ctx context.Context, a authz.Actor, up domain.Upload) (any, error) {
		return s.Domain.UploadToEvent(ctx, a, id, up)
	})
}

func (s *Server) uploadCommentAttachment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.upload(w, r, func(ctx context.Context, a authz.Actor, up domain.Upload) (any, error) {
		return s.Domain.UploadToComment(ctx, a, id, up)
	})
}

func (s *Server) uploadLogo(w http.ResponseWriter, r *http.Request) {
	s.upload(w, r, func(ctx context.Context, a authz.Actor, up domain.Upload) (any, error) {
		u, err := s.Domain.SetLogo(ctx, a, up)
		return map[string]string{"logoUrl": u}, err
	})
}

// --- downloads -------------------------------------------------------------

func (s *Server) attachmentContent(thumb bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, err := actorFrom(r.Context())
		if err != nil {
			writeProblem(w, toProblem(r.Context(), s.Log, err))
			return
		}
		c, err := s.Domain.OpenAttachment(r.Context(), a, chi.URLParam(r, "id"), thumb)
		if err != nil {
			writeProblem(w, toProblem(r.Context(), s.Log, err))
			return
		}
		defer c.Body.Close()
		attachments.ServeHeaders(w.Header(), c.ContentType, c.Filename, c.Inline)
		// ServeContent handles Range and conditional requests; Content-Type
		// is already set so it never sniffs.
		http.ServeContent(w, r, "", c.ModTime, c.Body)
	}
}

func (s *Server) logo(w http.ResponseWriter, r *http.Request) {
	c, err := s.Domain.OpenLogo(r.Context())
	if err != nil {
		writeProblem(w, toProblem(r.Context(), s.Log, err))
		return
	}
	defer c.Body.Close()
	attachments.ServeHeaders(w.Header(), c.ContentType, "logo", true)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	http.ServeContent(w, r, "", c.ModTime, c.Body)
}

// --- ICS feed --------------------------------------------------------------

func (s *Server) icsFeed(w http.ResponseWriter, r *http.Request) {
	file := chi.URLParam(r, "file")
	token, found := strings.CutSuffix(file, ".ics")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex")
	if !found {
		writeProblem(w, newProblem(http.StatusNotFound, "feed not found"))
		return
	}
	cal, err := s.Domain.Feed(r.Context(), token)
	if err != nil {
		writeProblem(w, toProblem(r.Context(), s.Log, err))
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="sharedcal.ics"`)
	w.Header().Set("Cache-Control", "private, max-age=300")
	if err := ics.Write(w, *cal); err != nil {
		s.Log.ErrorContext(r.Context(), "writing ICS feed failed", "err", err)
	}
}

// --- SPA -------------------------------------------------------------------

// spa serves the embedded React app. Hashed assets are public and cached
// forever; every other path gets index.html, but only for signed-in users
// (others are sent to the sign-in flow).
func (s *Server) spa(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeProblem(w, newProblem(http.StatusNotFound, "no such endpoint"))
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeProblem(w, newProblem(http.StatusMethodNotAllowed, "method not allowed"))
		return
	}
	if s.SPA == nil {
		http.Error(w, "web app not built", http.StatusNotFound)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name != "" && name != "index.html" {
		if st, err := fs.Stat(s.SPA, name); err == nil && !st.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=3600")
			}
			http.ServeFileFS(w, r, s.SPA, name)
			return
		}
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
	}
	if auth.FromContext(r.Context()) == nil {
		http.Redirect(w, r, "/auth/login?return="+urlQueryEscape(auth.SafeReturn(r.URL.RequestURI())), http.StatusFound)
		return
	}
	index, err := fs.ReadFile(s.SPA, "index.html")
	if err != nil {
		http.Error(w, "web app not built", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(index)
}

func urlQueryEscape(s string) string {
	r := strings.NewReplacer("%", "%25", "&", "%26", "+", "%2B", "#", "%23", " ", "%20", "?", "%3F", "=", "%3D")
	return r.Replace(s)
}

// documentRawOperations adds the non-huma endpoints to the OpenAPI document
// so the generated client knows about them.
func (s *Server) documentRawOperations() {
	oa := s.api.OpenAPI()
	sec := []map[string][]string{{"session": {}}}
	file := &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{
		"multipart/form-data": {Schema: &huma.Schema{Type: "object", Required: []string{"file"},
			Properties: map[string]*huma.Schema{"file": {Type: "string", Format: "binary"}}}},
	}}
	created := map[string]*huma.Response{
		"201": {Description: "Attachment", Content: map[string]*huma.MediaType{"application/json": {Schema: &huma.Schema{Ref: "#/components/schemas/AttachmentDTO"}}}},
		"413": {Description: "Too large"}, "415": {Description: "File type refused"}, "422": {Description: "Invalid file"},
	}
	idParam := []*huma.Param{{Name: "id", In: "path", Required: true, Schema: &huma.Schema{Type: "string"}}}
	binary := map[string]*huma.Response{"200": {Description: "File bytes", Content: map[string]*huma.MediaType{"application/octet-stream": {Schema: &huma.Schema{Type: "string", Format: "binary"}}}}}
	oa.AddOperation(&huma.Operation{OperationID: "uploadEventAttachment", Method: http.MethodPost, Path: "/api/events/{id}/attachments", Tags: []string{"attachments"}, Summary: "Upload a file to an event", Parameters: idParam, RequestBody: file, Responses: created, Security: sec})
	oa.AddOperation(&huma.Operation{OperationID: "uploadCommentAttachment", Method: http.MethodPost, Path: "/api/comments/{id}/attachments", Tags: []string{"attachments"}, Summary: "Upload a file to own comment", Parameters: idParam, RequestBody: file, Responses: created, Security: sec})
	oa.AddOperation(&huma.Operation{OperationID: "attachmentContent", Method: http.MethodGet, Path: "/api/attachments/{id}/content", Tags: []string{"attachments"}, Summary: "Download", Parameters: idParam, Responses: binary, Security: sec})
	oa.AddOperation(&huma.Operation{OperationID: "attachmentThumb", Method: http.MethodGet, Path: "/api/attachments/{id}/thumb", Tags: []string{"attachments"}, Summary: "Image thumbnail", Parameters: idParam, Responses: binary, Security: sec})
	oa.AddOperation(&huma.Operation{OperationID: "uploadLogo", Method: http.MethodPost, Path: "/api/settings/logo", Tags: []string{"settings"}, Summary: "Upload the branding logo", RequestBody: file, Responses: map[string]*huma.Response{"200": {Description: "Logo URL"}}, Security: sec})
	oa.AddOperation(&huma.Operation{OperationID: "stream", Method: http.MethodGet, Path: "/api/stream", Tags: []string{"realtime"}, Summary: "Server-Sent Events change stream",
		Responses: map[string]*huma.Response{"200": {Description: "text/event-stream", Content: map[string]*huma.MediaType{"text/event-stream": {Schema: &huma.Schema{Type: "string"}}}}}, Security: sec})
}
