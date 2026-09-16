package growth

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/seed"
)

// ---------- Step 1: Create Account (§25) ----------

type SignupInput struct {
	Email            string  `json:"email"`
	FullName         string  `json:"full_name"`
	Password         string  `json:"password"`
	OrganizationName string  `json:"organization_name"`
	Phone            *string `json:"phone"`
	SourcePage       *string `json:"source_page"`
	AnonymousID      *string `json:"anonymous_id"`
}

type SignupResult struct {
	SignupID  uuid.UUID `json:"signup_id"`
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
	// DevVerificationURL hanya diisi pada env local (tanpa SMTP) agar alur bisa diuji tanpa kotak masuk.
	DevVerificationURL string `json:"dev_verification_url,omitempty"`
}

func (s *Service) Signup(ctx context.Context, in SignupInput, ip, ua string) (*SignupResult, error) {
	if !s.limiter.Allow(ip) {
		return nil, apperr.RateLimited()
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.FullName = strings.TrimSpace(in.FullName)
	in.OrganizationName = strings.TrimSpace(in.OrganizationName)
	if !validEmail(in.Email) {
		return nil, apperr.Validation("Please enter a valid work email address").WithField("email", "invalid")
	}
	if in.FullName == "" {
		return nil, apperr.Validation("Please tell us your name").WithField("full_name", "required")
	}
	if len(in.Password) < 8 {
		return nil, apperr.Validation("Password must be at least 8 characters").WithField("password", "min 8")
	}
	if in.OrganizationName == "" {
		return nil, apperr.Validation("Please enter your organization or company name").WithField("organization_name", "required")
	}
	// email sudah dipakai akun aktif? (unik global, TAD; lookup tanpa org)
	var taken bool
	err := s.DB.WithAuthLookupTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email) = $1 AND deleted_at IS NULL)`, in.Email).Scan(&taken)
	})
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, apperr.Conflict("EMAIL_TAKEN", "An account with this email already exists. Try logging in instead.")
	}
	hash, err := iam.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	raw, tokenHash, err := newToken()
	if err != nil {
		return nil, err
	}
	res := &SignupResult{Email: in.Email, ExpiresAt: time.Now().Add(s.Cfg.SignupTokenTTL)}
	// signup pending sebelumnya untuk email yang sama dibatalkan (token lama tidak berlaku)
	if _, err := s.DB.Pool.Exec(ctx, `UPDATE signups SET expires_at = now() WHERE lower(email) = $1 AND verified_at IS NULL AND expires_at > now()`, in.Email); err != nil {
		return nil, err
	}
	if err := s.DB.Pool.QueryRow(ctx, `
		INSERT INTO signups (email, full_name, password_hash, organization_name, phone, token_hash, expires_at, source_page, ip, user_agent)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::inet,NULLIF($10,'')) RETURNING id`,
		in.Email, in.FullName, hash, in.OrganizationName, in.Phone, tokenHash, res.ExpiresAt, in.SourcePage, ip, ua).Scan(&res.SignupID); err != nil {
		return nil, err
	}
	link := s.verifyURL(raw)
	if err := s.Mailer.Send(ctx, verifyEmail(in.FullName, link, s.Cfg.SignupTokenTTL)); err != nil && s.Log != nil {
		s.Log.Error("send verification email", "err", err)
	}
	if s.Cfg.Env == "local" {
		res.DevVerificationURL = link
	}
	s.track(ctx, "signup_started", nil, nil, in.AnonymousID, in.SourcePage, map[string]any{"organization": in.OrganizationName})
	s.track(ctx, "account_created", nil, nil, in.AnonymousID, in.SourcePage, nil)
	return res, nil
}

func (s *Service) verifyURL(raw string) string {
	return strings.TrimRight(s.Cfg.PublicURL, "/") + "/verify-email?token=" + raw
}

// Resend: kirim ulang link verifikasi untuk signup yang belum terverifikasi (maks 5x).
func (s *Service) Resend(ctx context.Context, email, ip string) error {
	if !s.limiter.Allow(ip) {
		return apperr.RateLimited()
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !validEmail(email) {
		return apperr.Validation("Please enter a valid email address").WithField("email", "invalid")
	}
	var id uuid.UUID
	var fullName string
	var resend int
	err := s.DB.Pool.QueryRow(ctx, `SELECT id, full_name, resend_count FROM signups WHERE lower(email) = $1 AND verified_at IS NULL ORDER BY created_at DESC LIMIT 1`, email).Scan(&id, &fullName, &resend)
	if err != nil {
		// jangan bocorkan keberadaan email: respons sukses tanpa kirim
		return nil
	}
	if resend >= 5 {
		return apperr.RateLimited()
	}
	raw, tokenHash, err := newToken()
	if err != nil {
		return err
	}
	exp := time.Now().Add(s.Cfg.SignupTokenTTL)
	if _, err := s.DB.Pool.Exec(ctx, `UPDATE signups SET token_hash = $2, expires_at = $3, resend_count = resend_count + 1, updated_at = now() WHERE id = $1`, id, tokenHash, exp); err != nil {
		return err
	}
	if err := s.Mailer.Send(ctx, verifyEmail(fullName, s.verifyURL(raw), s.Cfg.SignupTokenTTL)); err != nil && s.Log != nil {
		s.Log.Error("resend verification email", "err", err)
	}
	return nil
}

// ---------- Step 2: Verify Email → Organization + Trial Workspace (§25, §27, §31) ----------

type VerifyResult struct {
	Tokens         *iam.TokenPair
	Principal      *authctx.Principal
	OrganizationID uuid.UUID
	UserID         uuid.UUID
	AlreadyDone    bool
}

func (s *Service) Verify(ctx context.Context, rawToken, ip, ua string) (*VerifyResult, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, apperr.Validation("Verification token is missing").WithField("token", "required")
	}
	th := hashToken(rawToken)
	tx, err := s.DB.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var su struct {
		ID         uuid.UUID
		Email      string
		FullName   string
		PassHash   string
		OrgName    string
		Phone      *string
		ExpiresAt  time.Time
		VerifiedAt *time.Time
		OrgID      *uuid.UUID
		UserID     *uuid.UUID
		SourcePage *string
	}
	err = tx.QueryRow(ctx, `SELECT id, email, full_name, password_hash, organization_name, phone, expires_at, verified_at, organization_id, user_id, source_page FROM signups WHERE token_hash = $1 FOR UPDATE`, th).
		Scan(&su.ID, &su.Email, &su.FullName, &su.PassHash, &su.OrgName, &su.Phone, &su.ExpiresAt, &su.VerifiedAt, &su.OrgID, &su.UserID, &su.SourcePage)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.New(404, "SIGNUP_TOKEN_INVALID", "Verification link is invalid", "This verification link is not valid. Request a new one from the sign up page.")
		}
		return nil, err
	}
	if su.VerifiedAt != nil {
		return nil, apperr.New(409, "SIGNUP_ALREADY_VERIFIED", "Email already verified", "This email has already been verified. You can log in with your password.")
	}
	if time.Now().After(su.ExpiresAt) {
		return nil, apperr.New(410, "SIGNUP_TOKEN_EXPIRED", "Verification link expired", "This verification link has expired. Request a new one from the sign up page.")
	}
	// email masih bebas? (bisa dipakai akun lain di antara signup dan verifikasi)
	if _, err := tx.Exec(ctx, `SELECT set_config('app.auth_lookup', 'on', true)`); err != nil {
		return nil, err
	}
	var taken bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email) = $1 AND deleted_at IS NULL)`, su.Email).Scan(&taken); err != nil {
		return nil, err
	}
	if taken {
		return nil, apperr.Conflict("EMAIL_TAKEN", "An account with this email already exists. Try logging in instead.")
	}

	// Organization (trial workspace)
	slug, err := uniqueSlug(ctx, tx, slugify(su.OrgName))
	if err != nil {
		return nil, err
	}
	var n int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM organizations`).Scan(&n)
	now := time.Now().UTC()
	trialEnd := now.Add(time.Duration(s.Cfg.TrialDays) * 24 * time.Hour)
	var orgID uuid.UUID
	for attempt := 0; attempt < 3; attempt++ {
		code := "ORG-" + pad6(n+1+attempt)
		err = tx.QueryRow(ctx, `INSERT INTO organizations (code, slug, name, trial_status, trial_started_at, trial_ends_at, signup_source, settings)
			VALUES ($1,$2,$3,'trial',$4,$5,'website', jsonb_build_object('onboarding', jsonb_build_object('started_at', $4::timestamptz))) RETURNING id`,
			code, slug, su.OrgName, now, trialEnd).Scan(&orgID)
		if err == nil {
			break
		}
		if !db.IsUniqueViolation(err) {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.organization_id', $1, true)`, orgID.String()); err != nil {
		return nil, err
	}
	// Seed baseline organization (role sistem, equipment, SLA default, kategori SR, provider pembayaran manual)
	if err := s.IAM.SeedSystemRoles(ctx, tx, orgID); err != nil {
		return nil, err
	}
	if err := seed.SeedEquipment(ctx, tx, orgID); err != nil {
		return nil, err
	}
	if err := seed.SeedSLADefaults(ctx, tx, orgID); err != nil {
		return nil, err
	}
	if err := seed.SeedSRCategories(ctx, tx, orgID); err != nil { // termasuk kategori tenant profile-aware (P1)
		return nil, err
	}
	if err := seed.SeedPaymentProviders(ctx, tx, orgID); err != nil {
		return nil, err
	}
	// Admin user (Organization Admin) dengan password yang sudah di-hash saat signup; email terverifikasi.
	userID, err := createAdminUser(ctx, tx, orgID, su.Email, su.FullName, su.PassHash, su.Phone)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE organizations SET created_by = $2, updated_by = $2 WHERE id = $1`, orgID, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE signups SET verified_at = now(), organization_id = $2, user_id = $3, updated_at = now() WHERE id = $1`, su.ID, orgID, userID); err != nil {
		return nil, err
	}
	_ = audit.LogAs(ctx, tx, orgID, &userID, ip, ua, audit.AuditEntry{Action: AuditEmailVerified, EntityType: "user", EntityID: &userID, EntityLabel: su.Email})
	_ = audit.LogAs(ctx, tx, orgID, &userID, ip, ua, audit.AuditEntry{Action: AuditTrialStarted, EntityType: "organization", EntityID: &orgID, EntityLabel: su.OrgName,
		After: map[string]any{"trial_status": TrialActive, "trial_ends_at": trialEnd, "days": s.Cfg.TrialDays}})
	pair, err := s.IAM.IssueSessionTx(ctx, tx, userID, orgID, "web", ip, ua)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	principal, err := s.IAM.LoadPrincipal(ctx, userID, orgID)
	if err != nil {
		return nil, err
	}
	// Notifikasi (§32): Welcome + Your trial has started
	go s.sendTrialMail(context.WithoutCancel(ctx), su.Email, su.FullName, su.OrgName, trialEnd)
	s.track(ctx, "email_verified", &orgID, &userID, nil, su.SourcePage, nil)
	s.track(ctx, "organization_created", &orgID, &userID, nil, su.SourcePage, nil)
	s.track(ctx, "trial_started", &orgID, &userID, nil, su.SourcePage, map[string]any{"days": s.Cfg.TrialDays})
	return &VerifyResult{Tokens: pair, Principal: principal, OrganizationID: orgID, UserID: userID}, nil
}

func (s *Service) sendTrialMail(ctx context.Context, email, name, org string, trialEnd time.Time) {
	app := strings.TrimRight(s.Cfg.PublicURL, "/")
	if err := s.Mailer.Send(ctx, welcomeEmail(name, org, app)); err != nil && s.Log != nil {
		s.Log.Error("send welcome email", "err", err)
	}
	if err := s.Mailer.Send(ctx, trialStartedEmail(name, org, app, trialEnd, s.Cfg.TrialDays)); err != nil && s.Log != nil {
		s.Log.Error("send trial started email", "err", err)
	}
}

func createAdminUser(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, email, name, passHash string, phone *string) (uuid.UUID, error) {
	var code string
	if err := tx.QueryRow(ctx, `SELECT 'USR-' || lpad((count(*)+1)::text, 6, '0') FROM users WHERE organization_id = $1`, orgID).Scan(&code); err != nil {
		return uuid.Nil, err
	}
	username := strings.Split(email, "@")[0]
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO users (organization_id, user_code, email, username, full_name, phone, password_hash, email_verified_at, preferred_locale)
		VALUES ($1,$2,$3,$4,$5,$6,$7,now(),'en') RETURNING id`, orgID, code, email, username, name, phone, passHash).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	var roleID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM roles WHERE organization_id = $1 AND code = 'organization_admin'`, orgID).Scan(&roleID); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id, property_id, granted_by) VALUES ($1,$2,NULL,$1)`, id, roleID); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func pad6(n int) string {
	s := itoa(n)
	for len(s) < 6 {
		s = "0" + s
	}
	return s
}
