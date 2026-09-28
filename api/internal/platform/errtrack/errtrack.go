// Package errtrack: error tracking (PRD P0 v2 §24.4, ADR-011 "Sentry optional via DSN").
// Tanpa DSN → no-op (error tetap tercatat di log terstruktur). Dengan BV_SENTRY_DSN → event dikirim ke
// endpoint envelope Sentry (kompatibel juga dengan GlitchTip) secara asinkron, tanpa SDK tambahan.
package errtrack

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Event: satu error yang dilaporkan.
type Event struct {
	Message   string
	Level     string // error | warning | fatal
	Tags      map[string]string
	Extra     map[string]any
	Stack     string
	RequestID string
}

type Reporter interface {
	Capture(ctx context.Context, e Event)
}

type noop struct{}

func (noop) Capture(context.Context, Event) {}

var (
	mu     sync.RWMutex
	global Reporter = noop{}
)

// Init mengaktifkan pelaporan ke Sentry bila dsn terisi; mengembalikan true bila aktif.
func Init(dsn, environment, release, service string, log *slog.Logger) bool {
	if strings.TrimSpace(dsn) == "" {
		return false
	}
	s, err := newSentry(dsn, environment, release, service, log)
	if err != nil {
		if log != nil {
			log.Warn("errtrack: DSN tidak valid, error tracking nonaktif", "err", err)
		}
		return false
	}
	mu.Lock()
	global = s
	mu.Unlock()
	return true
}

// SetReporter: dipakai test.
func SetReporter(r Reporter) {
	mu.Lock()
	global = r
	mu.Unlock()
}

// Capture melaporkan error (tidak pernah memblokir request).
func Capture(ctx context.Context, e Event) {
	mu.RLock()
	r := global
	mu.RUnlock()
	if e.Level == "" {
		e.Level = "error"
	}
	r.Capture(ctx, e)
}

// ---------- Sentry envelope ----------

type sentry struct {
	endpoint, auth            string
	environment, release, svc string
	client                    *http.Client
	log                       *slog.Logger
	queue                     chan []byte
}

func newSentry(dsn, environment, release, service string, log *slog.Logger) (*sentry, error) {
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil || u.Host == "" {
		return nil, fmt.Errorf("format DSN: https://<key>@<host>/<project>")
	}
	project := strings.Trim(u.Path, "/")
	if project == "" {
		return nil, fmt.Errorf("project id kosong")
	}
	key := u.User.Username()
	s := &sentry{
		endpoint:    fmt.Sprintf("%s://%s/api/%s/envelope/", u.Scheme, u.Host, project),
		auth:        fmt.Sprintf("Sentry sentry_version=7, sentry_client=buildingvision-go/1.0, sentry_key=%s", key),
		environment: environment, release: release, svc: service,
		client: &http.Client{Timeout: 5 * time.Second}, log: log,
		queue: make(chan []byte, 256),
	}
	go s.loop()
	return s, nil
}

func (s *sentry) Capture(_ context.Context, e Event) {
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	eventID := hex.EncodeToString(id)
	tags := map[string]string{"service": s.svc}
	for k, v := range e.Tags {
		tags[k] = v
	}
	if e.RequestID != "" {
		tags["request_id"] = e.RequestID
	}
	ev := map[string]any{
		"event_id": eventID, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "platform": "go",
		"level": e.Level, "environment": s.environment, "release": s.release, "server_name": s.svc,
		"message": map[string]any{"formatted": e.Message}, "tags": tags, "extra": e.Extra,
		"contexts": map[string]any{"runtime": map[string]any{"name": "go", "version": runtime.Version()}},
	}
	if e.Stack != "" {
		ev["extra"] = mergeExtra(e.Extra, "stack", e.Stack)
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}
	var buf bytes.Buffer
	hdr, _ := json.Marshal(map[string]any{"event_id": eventID, "sent_at": time.Now().UTC().Format(time.RFC3339Nano)})
	item, _ := json.Marshal(map[string]any{"type": "event", "length": len(payload)})
	buf.Write(hdr)
	buf.WriteByte('\n')
	buf.Write(item)
	buf.WriteByte('\n')
	buf.Write(payload)
	buf.WriteByte('\n')
	select {
	case s.queue <- buf.Bytes():
	default: // antrean penuh → buang (jangan membebani request)
	}
}

func mergeExtra(extra map[string]any, k string, v any) map[string]any {
	out := map[string]any{}
	for kk, vv := range extra {
		out[kk] = vv
	}
	out[k] = v
	return out
}

func (s *sentry) loop() {
	for body := range s.queue {
		req, err := http.NewRequest(http.MethodPost, s.endpoint, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/x-sentry-envelope")
		req.Header.Set("X-Sentry-Auth", s.auth)
		resp, err := s.client.Do(req)
		if err != nil {
			if s.log != nil {
				s.log.Warn("errtrack: kirim gagal", "err", err)
			}
			continue
		}
		_ = resp.Body.Close()
	}
}
