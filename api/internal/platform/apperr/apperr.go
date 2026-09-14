// Package apperr mendefinisikan error domain yang dipetakan ke RFC 9457 problem+json (TAD §6.3).
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type Error struct {
	Status int          `json:"status"`
	Code   string       `json:"code"`
	Title  string       `json:"title"`
	Detail string       `json:"detail,omitempty"`
	Fields []FieldError `json:"errors,omitempty"`
	cause  error
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Detail)
	}
	return e.Code
}
func (e *Error) Unwrap() error              { return e.cause }
func (e *Error) WithCause(err error) *Error { e.cause = err; return e }
func (e *Error) WithField(field, msg string) *Error {
	e.Fields = append(e.Fields, FieldError{Field: field, Message: msg})
	return e
}

func New(status int, code, title, detail string) *Error {
	return &Error{Status: status, Code: code, Title: title, Detail: detail}
}

func NotFound(what string) *Error {
	return New(http.StatusNotFound, "NOT_FOUND", "Resource not found", what+" tidak ditemukan")
}
func Forbidden(detail string) *Error {
	if detail == "" {
		detail = "Anda tidak memiliki izin untuk aksi ini"
	}
	return New(http.StatusForbidden, "FORBIDDEN", "Forbidden", detail)
}
func Unauthorized(detail string) *Error {
	if detail == "" {
		detail = "Autentikasi diperlukan"
	}
	return New(http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", detail)
}
func Validation(detail string) *Error {
	return New(http.StatusBadRequest, "VALIDATION_ERROR", "Validation failed", detail)
}
func Conflict(code, detail string) *Error {
	return New(http.StatusConflict, code, "Conflict", detail)
}
func InvalidTransition(detail string) *Error {
	return New(http.StatusConflict, "WORKFLOW_INVALID_TRANSITION", "Invalid status transition", detail)
}
func StaleVersion() *Error {
	return New(http.StatusConflict, "STALE_VERSION", "Version conflict", "Object telah berubah; muat ulang dan coba lagi")
}
func Internal(err error) *Error {
	return New(http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error", "Terjadi kesalahan internal").WithCause(err)
}
func RateLimited() *Error {
	return New(http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests", "Terlalu banyak percobaan; coba lagi nanti")
}

// From mengonversi error apa pun ke *Error (Internal bila tidak dikenal).
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Internal(err)
}

func Is(err error, code string) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Code == code
	}
	return false
}
