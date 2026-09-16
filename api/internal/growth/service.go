// Package growth: self-serve signup, verifikasi email, pembuatan organization + trial workspace, onboarding checklist,
// sample data, siklus hidup trial, Book a Demo, dan event funnel (Website PRD v1.1 §24–§34, §40).
// Konteks operasional (profile) tetap dimiliki Property (PRD §26–§27): package ini hanya mengorkestrasi modul yang ada.
package growth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/platform/mailer"
)

// Trial status (§31)
const (
	TrialNone       = "none"
	TrialActive     = "trial"
	TrialEndingSoon = "trial_ending_soon"
	TrialExpired    = "trial_expired"
	TrialConverted  = "converted"
	TrialCancelled  = "cancelled"
)

// Audit action
const (
	AuditSignupCreated   = "SIGNUP_CREATED"
	AuditEmailVerified   = "EMAIL_VERIFIED"
	AuditTrialStarted    = "TRIAL_STARTED"
	AuditTrialConverted  = "TRIAL_CONVERTED"
	AuditTrialCancelled  = "TRIAL_CANCELLED"
	AuditTrialExpired    = "TRIAL_EXPIRED"
	AuditSampleDataAdded = "SAMPLE_DATA_ADDED"
)

type Config struct {
	Env                 string
	PublicURL           string // dashboard (app) URL
	WebsiteURL          string
	TrialDays           int
	TrialEndingSoonDays int
	SignupTokenTTL      time.Duration
	SalesEmail          string
	EventsEnabled       bool
}

type Service struct {
	DB     *db.DB
	Jobs   jobs.Enqueuer
	IAM    *iam.Service
	Mailer mailer.Mailer
	Log    *slog.Logger
	Cfg    Config

	limiter *ipLimiter
}

func New(d *db.DB, j jobs.Enqueuer, iamSvc *iam.Service, m mailer.Mailer, log *slog.Logger, cfg Config) *Service {
	if cfg.TrialDays <= 0 {
		cfg.TrialDays = 14
	}
	if cfg.TrialEndingSoonDays <= 0 {
		cfg.TrialEndingSoonDays = 3
	}
	if cfg.SignupTokenTTL <= 0 {
		cfg.SignupTokenTTL = 24 * time.Hour
	}
	if m == nil {
		m = mailer.LogMailer{Log: log}
	}
	return &Service{DB: d, Jobs: j, IAM: iamSvc, Mailer: m, Log: log, Cfg: cfg, limiter: newIPLimiter(10, time.Minute)}
}

// SetRateLimit: 0 = nonaktif (test).
func (s *Service) SetRateLimit(perMinute int) { s.limiter = newIPLimiter(perMinute, time.Minute) }

// ---------- rate limiter per IP (endpoint publik) ----------

type ipLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
	max  int
	win  time.Duration
}

func newIPLimiter(max int, win time.Duration) *ipLimiter {
	return &ipLimiter{hits: map[string][]time.Time{}, max: max, win: win}
}

func (l *ipLimiter) Allow(ip string) bool {
	if l == nil || l.max <= 0 || ip == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	var keep []time.Time
	for _, t := range l.hits[ip] {
		if now.Sub(t) < l.win {
			keep = append(keep, t)
		}
	}
	if len(keep) >= l.max {
		l.hits[ip] = keep
		return false
	}
	l.hits[ip] = append(keep, now)
	return true
}

// ---------- helpers ----------

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]{2,}$`)

func validEmail(e string) bool { return len(e) <= 254 && emailRe.MatchString(e) }

func newToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	if s == "" {
		s = "org"
	}
	return s
}

// uniqueSlug: tambahkan sufiks -2, -3, ... bila slug sudah dipakai.
func uniqueSlug(ctx context.Context, tx pgx.Tx, base string) (string, error) {
	slug := base
	for i := 2; i < 1000; i++ {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organizations WHERE slug = $1)`, slug).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return slug, nil
		}
		slug = base + "-" + itoa(i)
	}
	return base + "-" + uuid.NewString()[:8], nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func ptr[T any](v T) *T { return &v }
