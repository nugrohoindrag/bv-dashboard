// Package httpx: helper HTTP — problem+json (RFC 9457), JSON decode/encode, request id,
// logging, recover, cursor pagination (TAD §6.1, §6.3).
package httpx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/platform/apperr"
)

const (
	HeaderRequestID      = "X-Request-Id"
	HeaderIdempotencyKey = "Idempotency-Key"
	HeaderIfMatch        = "If-Match"
	ProblemBaseURL       = "https://docs.buildingvision.id/errors/"
	DefaultLimit         = 25
	MaxLimit             = 200
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// RequestIDMiddleware: echo/generate X-Request-Id.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get(HeaderRequestID)
		if rid == "" || len(rid) > 128 {
			rid = uuid.NewString()
		}
		w.Header().Set(HeaderRequestID, rid)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, rid)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }
func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = 200
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// LoggingMiddleware: structured JSON log per request (TAD §11.5).
func LoggingMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = 200
			}
			lvl := slog.LevelInfo
			if rec.status >= 500 {
				lvl = slog.LevelError
			} else if rec.status >= 400 {
				lvl = slog.LevelWarn
			}
			log.LogAttrs(r.Context(), lvl, "http_request",
				slog.String("request_id", RequestID(r.Context())),
				slog.String("method", r.Method),
				slog.String("route", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Duration("latency", time.Since(start)),
				slog.String("ip", ClientIP(r)),
			)
		})
	}
}

func RecoverMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic", slog.Any("panic", rec), slog.String("stack", string(debug.Stack())), slog.String("request_id", RequestID(r.Context())))
					WriteError(w, r, apperr.Internal(fmt.Errorf("panic: %v", rec)))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func CORS(origins []string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range origins {
		allowed[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (allowed[origin] || allowed["*"]) {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Vary", "Origin")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-Id, Idempotency-Key, If-Match, X-Property-Id")
				h.Set("Access-Control-Expose-Headers", "X-Request-Id, ETag")
				h.Set("Access-Control-Max-Age", "600")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func ClientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		return strings.TrimSpace(strings.Split(xf, ",")[0])
	}
	if xr := r.Header.Get("X-Real-Ip"); xr != "" {
		return xr
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return strings.Trim(host, "[]")
}

// ---------- JSON ----------

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func Decode(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 2<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return apperr.Validation("body kosong")
		}
		return apperr.Validation("JSON tidak valid: " + err.Error())
	}
	return nil
}

type problem struct {
	Type      string              `json:"type"`
	Title     string              `json:"title"`
	Status    int                 `json:"status"`
	Detail    string              `json:"detail,omitempty"`
	Code      string              `json:"code"`
	Errors    []apperr.FieldError `json:"errors,omitempty"`
	RequestID string              `json:"request_id,omitempty"`
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	e := apperr.From(err)
	if e.Status >= 500 {
		slog.Default().Error("request_error", slog.String("request_id", RequestID(r.Context())), slog.String("err", fmt.Sprint(errors.Unwrap(e))), slog.String("code", e.Code))
	}
	p := problem{
		Type:      ProblemBaseURL + strings.ToLower(strings.ReplaceAll(e.Code, "_", "-")),
		Title:     e.Title,
		Status:    e.Status,
		Detail:    e.Detail,
		Code:      e.Code,
		Errors:    e.Fields,
		RequestID: RequestID(r.Context()),
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// ---------- Pagination ----------

type Page struct {
	Limit  int
	Cursor *Cursor
}

// Cursor: (sort_value, id) encoded base64url. sort_value bebas (waktu RFC3339Nano atau string).
type Cursor struct {
	Value string    `json:"v"`
	ID    uuid.UUID `json:"id"`
}

func EncodeCursor(value string, id uuid.UUID) string {
	b, _ := json.Marshal(Cursor{Value: value, ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func DecodeCursor(s string) (*Cursor, error) {
	if s == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, apperr.Validation("cursor tidak valid")
	}
	var c Cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, apperr.Validation("cursor tidak valid")
	}
	return &c, nil
}

func ParsePage(r *http.Request) (Page, error) {
	p := Page{Limit: DefaultLimit}
	if l := r.URL.Query().Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 {
			return p, apperr.Validation("limit harus angka positif")
		}
		if n > MaxLimit {
			n = MaxLimit
		}
		p.Limit = n
	}
	c, err := DecodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		return p, err
	}
	p.Cursor = c
	return p, nil
}

type ListResponse[T any] struct {
	Data       []T     `json:"data"`
	NextCursor *string `json:"next_cursor"`
	Total      *int    `json:"total,omitempty"`
}

func NewList[T any](items []T, next *string) ListResponse[T] {
	if items == nil {
		items = []T{}
	}
	return ListResponse[T]{Data: items, NextCursor: next}
}

// ---------- Query helpers ----------

func QueryUUID(r *http.Request, key string) (*uuid.UUID, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil, nil
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return nil, apperr.Validation(key + " harus UUID")
	}
	return &id, nil
}

func QueryTime(r *http.Request, key string) (*time.Time, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		// fallback tanggal saja
		d, err2 := time.Parse("2006-01-02", v)
		if err2 != nil {
			return nil, apperr.Validation(key + " harus ISO 8601")
		}
		t = d
	}
	return &t, nil
}

func QueryCSV(r *http.Request, key string) []string {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil
	}
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func PathUUID(r *http.Request, param func(*http.Request, string) string, key string) (uuid.UUID, error) {
	id, err := uuid.Parse(param(r, key))
	if err != nil {
		return uuid.Nil, apperr.Validation(key + " harus UUID")
	}
	return id, nil
}

// IfMatchVersion membaca header If-Match sebagai version (optimistic locking, TAD §6.4).
func IfMatchVersion(r *http.Request) *int {
	v := strings.Trim(r.Header.Get(HeaderIfMatch), `"`)
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}
	return &n
}
