package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/felixtrav/sharedcal/internal/attachments"
	"github.com/felixtrav/sharedcal/internal/auth"
	"github.com/felixtrav/sharedcal/internal/domain"
)

// Problem is an RFC 9457 problem document with SharedCal's extensions.
type Problem struct {
	huma.ErrorModel
	// Suggest is set on 403 when the caller may send a suggestion instead.
	Suggest bool `json:"suggest,omitempty"`
	// Current is the current resource on 409 conflicts.
	Current any `json:"current,omitempty"`
}

func (p *Problem) Error() string                { return p.Detail }
func (p *Problem) GetStatus() int               { return p.Status }
func (p *Problem) ContentType(ct string) string { return p.ErrorModel.ContentType(ct) }
func newProblem(status int, detail string) *Problem {
	return &Problem{ErrorModel: huma.ErrorModel{Status: status, Title: http.StatusText(status), Detail: detail}}
}

func init() {
	// The server always sends arrays (never null), so the schema says so and
	// the generated TypeScript types are exact.
	huma.DefaultArrayNullable = false
	// Every huma-generated error (validation, body too large…) uses Problem.
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		p := newProblem(status, msg)
		for _, e := range errs {
			if e != nil {
				p.Add(e)
			}
		}
		return p
	}
}

// toProblem converts any error into a problem, hiding internal details.
func toProblem(ctx context.Context, log *slog.Logger, err error) *Problem {
	var p *Problem
	if errors.As(err, &p) {
		return p
	}
	var de *domain.Error
	if errors.As(err, &de) {
		var status int
		switch de.Kind {
		case domain.KindInvalid:
			status = http.StatusUnprocessableEntity
		case domain.KindNotFound:
			status = http.StatusNotFound
		case domain.KindForbidden:
			status = http.StatusForbidden
		case domain.KindConflict:
			status = http.StatusConflict
		case domain.KindPrecondition:
			status = http.StatusPreconditionRequired
		case domain.KindLimit:
			status = http.StatusUnprocessableEntity
		case domain.KindUnauthenticated:
			status = http.StatusUnauthorized
		default:
			status = http.StatusInternalServerError
		}
		p = newProblem(status, de.Msg)
		for _, f := range de.Fields {
			p.Errors = append(p.Errors, &huma.ErrorDetail{Message: f.Message, Location: f.Field})
		}
		p.Suggest, p.Current = de.Suggest, de.Current
		return p
	}
	var mbe *http.MaxBytesError
	switch {
	case errors.As(err, &mbe), errors.Is(err, attachments.ErrTooLarge):
		return newProblem(http.StatusRequestEntityTooLarge, "the file is larger than the upload limit")
	case errors.Is(err, attachments.ErrTypeNotAllowed):
		return newProblem(http.StatusUnsupportedMediaType, "this file type is not allowed")
	case errors.Is(err, attachments.ErrTypeMismatch):
		return newProblem(http.StatusUnsupportedMediaType, "the file's contents do not match its extension")
	case errors.Is(err, attachments.ErrEmpty):
		return newProblem(http.StatusUnprocessableEntity, "the file is empty")
	case errors.Is(err, attachments.ErrImageTooLarge):
		return newProblem(http.StatusUnprocessableEntity, "the image has too many pixels")
	case errors.Is(err, attachments.ErrCorrupt):
		return newProblem(http.StatusUnprocessableEntity, "the file is corrupt or malformed")
	case errors.Is(err, auth.ErrCSRF):
		return newProblem(http.StatusForbidden, "missing or invalid CSRF token")
	case errors.Is(err, context.Canceled):
		return newProblem(499, "request cancelled")
	}
	if log != nil {
		log.ErrorContext(ctx, "internal error", "err", err)
	}
	return newProblem(http.StatusInternalServerError, "internal server error")
}

// writeProblem writes a problem from a plain (non-huma) handler.
func writeProblem(w http.ResponseWriter, p *Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func errUnauthenticated() *Problem { return newProblem(http.StatusUnauthorized, "sign in required") }
