package growth

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
)

// ---------- Funnel analytics (§40): tanpa data pribadi; event dari website & dashboard ----------

// AllowedEvents: daftar event PRD §40 (+ beberapa event internal). Event lain ditolak agar tabel tetap bersih.
var AllowedEvents = map[string]struct{}{
	"page_viewed": {}, "solution_viewed": {}, "pricing_viewed": {}, "download_apps_viewed": {}, "app_download_clicked": {},
	"start_trial_clicked": {}, "signup_started": {}, "account_created": {}, "email_verified": {}, "organization_created": {},
	"profile_selected": {}, "property_created": {}, "trial_started": {}, "dashboard_opened": {}, "request_created": {},
	"task_created": {}, "work_order_created": {}, "staff_invited": {}, "task_completed": {}, "evidence_added": {},
	"request_resolved": {}, "tenant_invited": {}, "tenant_app_activated": {}, "subscription_started": {}, "demo_requested": {},
	"login_clicked": {}, "book_demo_clicked": {}, "sample_data_added": {}, "trial_activated": {}, "onboarding_completed": {},
}

type EventInput struct {
	Event       string         `json:"event"`
	SourcePage  *string        `json:"source_page"`
	AnonymousID *string        `json:"anonymous_id"`
	Properties  map[string]any `json:"properties"`
	OccurredAt  *time.Time     `json:"occurred_at"`
}

// TrackPublic: dari website (tanpa auth). Rate-limit per IP; properti dibatasi ukurannya.
func (s *Service) TrackPublic(ctx context.Context, events []EventInput, ip string) (int, error) {
	if !s.Cfg.EventsEnabled {
		return 0, nil
	}
	if !s.limiter.Allow(ip) {
		return 0, apperr.RateLimited()
	}
	if len(events) > 20 {
		events = events[:20]
	}
	n := 0
	for _, e := range events {
		if s.trackRaw(ctx, e, nil, nil) {
			n++
		}
	}
	return n, nil
}

// TrackUser: dari dashboard (auth) — organization/user dilampirkan dari principal.
func (s *Service) TrackUser(ctx context.Context, events []EventInput) (int, error) {
	if !s.Cfg.EventsEnabled {
		return 0, nil
	}
	p := authctx.Must(ctx)
	if len(events) > 20 {
		events = events[:20]
	}
	n := 0
	for _, e := range events {
		if s.trackRaw(ctx, e, &p.OrganizationID, &p.UserID) {
			n++
		}
	}
	return n, nil
}

func (s *Service) track(ctx context.Context, event string, orgID, userID *uuid.UUID, anon, source *string, props map[string]any) {
	if !s.Cfg.EventsEnabled {
		return
	}
	s.trackRaw(context.WithoutCancel(ctx), EventInput{Event: event, SourcePage: source, AnonymousID: anon, Properties: props}, orgID, userID)
}

func (s *Service) trackRaw(ctx context.Context, e EventInput, orgID, userID *uuid.UUID) bool {
	e.Event = strings.ToLower(strings.TrimSpace(e.Event))
	if _, ok := AllowedEvents[e.Event]; !ok {
		return false
	}
	if e.Properties == nil {
		e.Properties = map[string]any{}
	}
	b, err := json.Marshal(e.Properties)
	if err != nil || len(b) > 4096 {
		b = []byte("{}")
	}
	occurred := time.Now().UTC()
	if e.OccurredAt != nil && time.Since(*e.OccurredAt) < 24*time.Hour && e.OccurredAt.Before(time.Now().Add(5*time.Minute)) {
		occurred = e.OccurredAt.UTC()
	}
	var anon *string
	if e.AnonymousID != nil && len(*e.AnonymousID) <= 64 {
		anon = e.AnonymousID
	}
	var source *string
	if e.SourcePage != nil && len(*e.SourcePage) <= 512 {
		source = e.SourcePage
	}
	_, err = s.DB.Pool.Exec(ctx, `INSERT INTO growth_events (event, organization_id, user_id, anonymous_id, source_page, properties, occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		e.Event, orgID, userID, anon, source, b, occurred)
	if err != nil && s.Log != nil {
		s.Log.Warn("growth event", "event", e.Event, "err", err)
	}
	return err == nil
}

// ---------- Book a Demo (§34) ----------

type DemoRequestInput struct {
	FullName        string  `json:"full_name"`
	Email           string  `json:"email"`
	Company         *string `json:"company"`
	Phone           *string `json:"phone"`
	PropertyProfile *string `json:"property_profile"`
	PropertyCount   *int    `json:"property_count"`
	Message         *string `json:"message"`
	SourcePage      *string `json:"source_page"`
	AnonymousID     *string `json:"anonymous_id"`
}

func (s *Service) RequestDemo(ctx context.Context, in DemoRequestInput, ip string) (uuid.UUID, error) {
	if !s.limiter.Allow(ip) {
		return uuid.Nil, apperr.RateLimited()
	}
	in.FullName = strings.TrimSpace(in.FullName)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.FullName == "" {
		return uuid.Nil, apperr.Validation("Please tell us your name").WithField("full_name", "required")
	}
	if !validEmail(in.Email) {
		return uuid.Nil, apperr.Validation("Please enter a valid email address").WithField("email", "invalid")
	}
	if in.PropertyProfile != nil {
		v := strings.ToLower(strings.TrimSpace(*in.PropertyProfile))
		if v == "" {
			in.PropertyProfile = nil
		} else if v != "hotel" && v != "apartment" && v != "office" {
			return uuid.Nil, apperr.Validation("Property profile must be hotel, apartment, or office").WithField("property_profile", "invalid")
		} else {
			in.PropertyProfile = &v
		}
	}
	if in.Message != nil && len(*in.Message) > 2000 {
		return uuid.Nil, apperr.Validation("Message is too long").WithField("message", "max 2000")
	}
	var id uuid.UUID
	if err := s.DB.Pool.QueryRow(ctx, `INSERT INTO demo_requests (full_name, email, company, phone, property_profile, property_count, message, source_page, ip) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::inet) RETURNING id`,
		in.FullName, in.Email, in.Company, in.Phone, in.PropertyProfile, in.PropertyCount, in.Message, in.SourcePage, ip).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		if s.Cfg.SalesEmail != "" {
			_ = s.Mailer.Send(bg, withTo(demoRequestEmail(in), s.Cfg.SalesEmail))
		}
		_ = s.Mailer.Send(bg, withTo(demoConfirmationEmail(in.FullName), in.Email))
	}()
	s.track(ctx, "demo_requested", nil, nil, in.AnonymousID, in.SourcePage, map[string]any{"profile": in.PropertyProfile})
	return id, nil
}
