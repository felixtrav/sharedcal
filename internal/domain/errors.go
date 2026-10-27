// Package domain implements SharedCal's business rules on top of the store.
//
// Every mutation runs in one database transaction that also writes the
// activity row(s) and change_log row(s); after commit the change messages are
// published to the realtime broker. Every operation receives the caller as an
// authz.Actor and asks package authz before doing anything.
package domain

import (
	"errors"
	"fmt"
)

// Kind classifies domain errors for the HTTP layer.
type Kind int

const (
	KindInvalid Kind = iota + 1
	KindNotFound
	KindForbidden
	KindConflict
	KindPrecondition
	KindLimit
	KindUnauthenticated
)

// FieldError describes one invalid input field.
type FieldError struct {
	Field   string `json:"location"`
	Message string `json:"message"`
}

// Error is a typed domain error.
type Error struct {
	Kind    Kind
	Msg     string
	Fields  []FieldError
	Current any  // for KindConflict: the current version of the resource
	Suggest bool // for KindForbidden: the caller may suggest instead
}

func (e *Error) Error() string { return e.Msg }

func invalid(msg string, fields ...FieldError) *Error {
	return &Error{Kind: KindInvalid, Msg: msg, Fields: fields}
}

func invalidField(field, format string, args ...any) *Error {
	msg := fmt.Sprintf(format, args...)
	return &Error{Kind: KindInvalid, Msg: msg, Fields: []FieldError{{Field: field, Message: msg}}}
}

func notFound(what string) *Error { return &Error{Kind: KindNotFound, Msg: what + " not found"} }

func forbidden(msg string) *Error { return &Error{Kind: KindForbidden, Msg: msg} }

func conflict(msg string, current any) *Error {
	return &Error{Kind: KindConflict, Msg: msg, Current: current}
}

func limit(msg string) *Error { return &Error{Kind: KindLimit, Msg: msg} }

// ErrorKind returns the Kind of err, or 0 for unexpected errors.
func ErrorKind(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return 0
}

// fieldErrors accumulates validation errors.
type fieldErrors []FieldError

func (f *fieldErrors) add(field, format string, args ...any) {
	*f = append(*f, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
}

func (f fieldErrors) err() error {
	if len(f) == 0 {
		return nil
	}
	return invalid(f[0].Message, f...)
}
