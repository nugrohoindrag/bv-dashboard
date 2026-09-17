package bvrooms

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/httpx"
)

// ---------- token customer (issuer bvrooms; kunci Ed25519 sama dengan staf, issuer/audience berbeda) ----------

const (
	issuer      = "bvrooms"
	audCustomer = "bvrooms_customer"
	audOTP      = "bvrooms_otp"
)

type customerClaims struct {
	jwt.RegisteredClaims
	Org     string `json:"org"`
	Sid     string `json:"sid,omitempty"`
	Purpose string `json:"purpose,omitempty"` // token OTP: register | change_phone
	Phone   string `json:"phone,omitempty"`
}

type customerSigner struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

func newCustomerSigner(priv ed25519.PrivateKey, pub ed25519.PublicKey) *customerSigner {
	return &customerSigner{priv: priv, pub: pub}
}

func (c *customerSigner) sign(claims customerClaims) (string, error) {
	claims.Issuer = issuer
	claims.ID = uuid.NewString()
	return jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(c.priv)
}

func (c *customerSigner) verify(token, aud string) (*customerClaims, error) {
	var claims customerClaims
	_, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return c.pub, nil
	}, jwt.WithIssuer(issuer), jwt.WithAudience(aud), jwt.WithLeeway(30*time.Second))
	if err != nil {
		return nil, err
	}
	return &claims, nil
}

// Authenticate: middleware token customer → principal Source=bvrooms (UserID = customer id).
func (s *Service) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			httpx.WriteError(w, r, apperr.Unauthorized("Authorization bearer token diperlukan"))
			return
		}
		claims, err := s.signer.verify(strings.TrimPrefix(h, "Bearer "), audCustomer)
		if err != nil {
			httpx.WriteError(w, r, apperr.Unauthorized("token tidak valid atau kedaluwarsa"))
			return
		}
		cid, err1 := uuid.Parse(claims.Subject)
		org, err2 := uuid.Parse(claims.Org)
		sid, _ := uuid.Parse(claims.Sid)
		if err1 != nil || err2 != nil {
			httpx.WriteError(w, r, apperr.Unauthorized("token tidak valid"))
			return
		}
		var name, status string
		var revoked *time.Time
		err = s.DB.WithOrgTx(r.Context(), org, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT c.full_name, c.status, se.revoked_at FROM bvrooms_customers c JOIN bvrooms_sessions se ON se.customer_id = c.id AND se.id = $2 WHERE c.id = $1`, cid, sid).Scan(&name, &status, &revoked)
		})
		if err != nil || revoked != nil {
			httpx.WriteError(w, r, apperr.Unauthorized("sesi tidak berlaku"))
			return
		}
		if status != "active" {
			httpx.WriteError(w, r, apperr.Forbidden("Akun diblokir"))
			return
		}
		p := &authctx.Principal{UserID: cid, OrganizationID: org, SessionID: sid, FullName: name, Source: authctx.SourceBVRooms,
			RequestID: httpx.RequestID(r.Context()), IP: httpx.ClientIP(r), UserAgent: r.UserAgent()}
		next.ServeHTTP(w, r.WithContext(authctx.With(r.Context(), p)))
	})
}

// ---------- OTP ----------

type OTPRequestInput struct {
	OrganizationSlug string `json:"organization_slug"`
	Phone            string `json:"phone"`
	Purpose          string `json:"purpose"` // login | register | change_phone
}

type OTPRequestResult struct {
	ExpiresIn   int    `json:"expires_in"`
	ResendAfter int    `json:"resend_after"`
	MaskedPhone string `json:"masked_phone"`
	Provider    string `json:"provider"`
	DevCode     string `json:"dev_code,omitempty"` // hanya env local/test (provider mock)
}

var otpPurposes = map[string]bool{"login": true, "register": true, "change_phone": true}

// RequestOTP: kirim kode 4 digit (TTL 100 s, resend 60 s, 5 request/nomor/jam, 20/IP/jam). login → 404 bila belum
// terdaftar (Figma "nomor tidak terdaftar"); register → 409 bila sudah ada.
func (s *Service) RequestOTP(ctx context.Context, in OTPRequestInput, ip string) (*OTPRequestResult, error) {
	if !otpPurposes[in.Purpose] {
		return nil, apperr.Validation("purpose harus login|register|change_phone")
	}
	phone, err := NormalizePhone(in.Phone)
	if err != nil {
		return nil, err
	}
	orgID, _, err := s.ResolveOrg(ctx, in.OrganizationSlug)
	if err != nil {
		return nil, err
	}
	if !s.ipLimiter.Allow(ip) {
		return nil, apperr.RateLimited()
	}
	code, err := randomDigits(4)
	if err != nil {
		return nil, err
	}
	if s.Cfg.OTPStaticCode != "" {
		code = s.Cfg.OTPStaticCode
	}
	now := s.now()
	err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var exists bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM bvrooms_customers WHERE phone_e164 = $1)`, phone).Scan(&exists)
		switch in.Purpose {
		case "login":
			if !exists {
				return apperr.New(404, "PHONE_NOT_REGISTERED", "Phone not registered", "Nomor belum terdaftar; silakan daftar terlebih dahulu")
			}
		case "register", "change_phone":
			if exists {
				return apperr.Conflict("PHONE_EXISTS", "Nomor sudah terdaftar; silakan masuk")
			}
		}
		var last *time.Time
		var perHour int
		_ = tx.QueryRow(ctx, `SELECT max(created_at), count(*) FILTER (WHERE created_at > $2) FROM bvrooms_otp_codes WHERE phone_e164 = $1`, phone, now.Add(-time.Hour)).Scan(&last, &perHour)
		if last != nil && now.Sub(*last) < s.Cfg.OTPResend {
			return apperr.New(429, "OTP_RESEND_TOO_SOON", "Too many requests", "Tunggu sebelum meminta kode ulang")
		}
		if perHour >= 5 {
			return apperr.New(429, "OTP_RATE_LIMITED", "Too many requests", "Batas permintaan OTP tercapai; coba lagi nanti")
		}
		id := uuid.Must(uuid.NewV7())
		_, err := tx.Exec(ctx, `INSERT INTO bvrooms_otp_codes (id, organization_id, phone_e164, purpose, code_hash, expires_at, ip) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			id, orgID, phone, in.Purpose, hashCode(id, code), now.Add(s.Cfg.OTPTTL), ip)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := s.SMS.SendOTP(ctx, phone, code, s.Cfg.OTPTTL); err != nil {
		s.Log.Warn("bvrooms sms send", "err", err)
	}
	out := &OTPRequestResult{ExpiresIn: int(s.Cfg.OTPTTL.Seconds()), ResendAfter: int(s.Cfg.OTPResend.Seconds()), MaskedPhone: MaskPhone(phone), Provider: s.SMS.Code()}
	if (s.devMode() || s.Cfg.OTPExposeCode) && s.SMS.Code() == "mock" {
		out.DevCode = code
	}
	return out, nil
}

type OTPVerifyInput struct {
	OrganizationSlug string `json:"organization_slug"`
	Phone            string `json:"phone"`
	Code             string `json:"code"`
	Purpose          string `json:"purpose"`
	DeviceID         string `json:"device_id"`
}

type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
}

type Customer struct {
	ID        uuid.UUID  `json:"id"`
	FullName  string     `json:"full_name"`
	Phone     string     `json:"phone"`
	Email     *string    `json:"email"`
	Status    string     `json:"status"`
	Locale    string     `json:"locale"`
	CreatedAt time.Time  `json:"created_at"`
	LastLogin *time.Time `json:"last_login_at"`
}

type AuthResult struct {
	*TokenPair
	Customer     *Customer `json:"customer,omitempty"`
	OTPToken     string    `json:"otp_token,omitempty"` // register / change_phone (5 menit)
	PINIsDefault bool      `json:"pin_is_default"`      // login PIN: akun masih memakai PIN default → ajak ganti PIN
}

// VerifyOTP: login → token sesi; register/change_phone → otp_token untuk langkah berikutnya.
func (s *Service) VerifyOTP(ctx context.Context, in OTPVerifyInput, ip, ua string) (*AuthResult, error) {
	if !otpPurposes[in.Purpose] {
		return nil, apperr.Validation("purpose harus login|register|change_phone")
	}
	code := strings.TrimSpace(in.Code)
	if len(code) != 4 {
		return nil, apperr.Validation("kode OTP 4 digit wajib").WithField("code", "wajib")
	}
	phone, err := NormalizePhone(in.Phone)
	if err != nil {
		return nil, err
	}
	orgID, _, err := s.ResolveOrg(ctx, in.OrganizationSlug)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var out *AuthResult
	err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var id uuid.UUID
		var hash string
		var exp time.Time
		var attempts int
		var consumed *time.Time
		err := tx.QueryRow(ctx, `SELECT id, code_hash, expires_at, attempts, consumed_at FROM bvrooms_otp_codes WHERE phone_e164 = $1 AND purpose = $2 ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, phone, in.Purpose).Scan(&id, &hash, &exp, &attempts, &consumed)
		if err != nil {
			return apperr.New(401, "OTP_INVALID", "OTP invalid", "Kode OTP tidak valid")
		}
		if consumed != nil || now.After(exp) {
			return apperr.New(410, "OTP_EXPIRED", "OTP expired", "Kode OTP kedaluwarsa; minta kode baru")
		}
		if attempts >= 5 {
			return apperr.New(423, "OTP_LOCKED", "OTP locked", "Terlalu banyak percobaan; minta kode baru")
		}
		if hashCode(id, code) != hash {
			_, _ = tx.Exec(ctx, `UPDATE bvrooms_otp_codes SET attempts = attempts + 1 WHERE id = $1`, id)
			return apperr.New(401, "OTP_INVALID", "OTP invalid", "Kode OTP tidak valid")
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_otp_codes SET consumed_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		if in.Purpose != "login" {
			tok, err := s.signer.sign(customerClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: phone, Audience: jwt.ClaimStrings{audOTP}, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute))}, Org: orgID.String(), Purpose: in.Purpose, Phone: phone})
			if err != nil {
				return err
			}
			out = &AuthResult{OTPToken: tok}
			return nil
		}
		c, err := s.customerByPhoneTx(ctx, tx, phone)
		if err != nil {
			return err
		}
		if c.Status != "active" {
			return apperr.Forbidden("Akun diblokir")
		}
		pair, err := s.issueSessionTx(ctx, tx, orgID, c.ID, in.DeviceID, ip, ua, now)
		if err != nil {
			return err
		}
		_, _ = tx.Exec(ctx, `UPDATE bvrooms_customers SET last_login_at = now() WHERE id = $1`, c.ID)
		out = &AuthResult{TokenPair: pair, Customer: c}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type RegisterInput struct {
	OrganizationSlug string `json:"organization_slug"`
	OTPToken         string `json:"otp_token"`
	FullName         string `json:"full_name"`
	Email            string `json:"email"`
	DeviceID         string `json:"device_id"`
}

// Register: buat customer setelah OTP terverifikasi (Figma "Lengkapi data dirimu").
func (s *Service) Register(ctx context.Context, in RegisterInput, ip, ua string) (*AuthResult, error) {
	claims, err := s.signer.verify(in.OTPToken, audOTP)
	if err != nil || claims.Purpose != "register" {
		return nil, apperr.Unauthorized("otp_token tidak valid; ulangi verifikasi OTP")
	}
	orgID, _, err := s.ResolveOrg(ctx, in.OrganizationSlug)
	if err != nil {
		return nil, err
	}
	if claims.Org != orgID.String() {
		return nil, apperr.Unauthorized("otp_token tidak valid untuk organization ini")
	}
	name := strings.TrimSpace(in.FullName)
	if name == "" {
		return nil, apperr.Validation("Nama lengkap tidak boleh kosong!").WithField("full_name", "wajib")
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" {
		return nil, apperr.Validation("Email tidak boleh kosong!").WithField("email", "wajib")
	}
	if !emailRe.MatchString(email) {
		return nil, apperr.Validation("email tidak valid").WithField("email", "tidak valid")
	}
	now := s.now()
	var out *AuthResult
	err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO bvrooms_customers (organization_id, phone_e164, full_name, email, last_login_at) VALUES ($1,$2,$3,$4,now()) RETURNING id`, orgID, claims.Phone, name, email).Scan(&id); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("PHONE_EXISTS", "Nomor sudah terdaftar; silakan masuk")
			}
			return err
		}
		c, err := s.customerByIDTx(ctx, tx, id)
		if err != nil {
			return err
		}
		pair, err := s.issueSessionTx(ctx, tx, orgID, id, in.DeviceID, ip, ua, now)
		if err != nil {
			return err
		}
		_ = audit.LogAs(ctx, tx, orgID, nil, ip, ua, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "bvrooms_customer", EntityID: &id, EntityLabel: MaskPhone(claims.Phone), After: map[string]any{"source": "bvrooms_app"}})
		out = &AuthResult{TokenPair: pair, Customer: c}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) issueSessionTx(ctx context.Context, tx pgx.Tx, orgID, customerID uuid.UUID, deviceID, ip, ua string, now time.Time) (*TokenPair, error) {
	raw, hash, err := iam.NewRefreshToken()
	if err != nil {
		return nil, err
	}
	sid := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_sessions (id, organization_id, customer_id, device_id, refresh_hash, user_agent, ip, expires_at) VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8)`,
		sid, orgID, customerID, deviceID, hash, ua, ip, now.Add(s.Cfg.RefreshTTL)); err != nil {
		return nil, err
	}
	exp := now.Add(s.Cfg.AccessTTL)
	tok, err := s.signer.sign(customerClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: customerID.String(), Audience: jwt.ClaimStrings{audCustomer}, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(exp)}, Org: orgID.String(), Sid: sid.String()})
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: tok, RefreshToken: raw, ExpiresAt: exp, TokenType: "Bearer"}, nil
}

// Refresh: rotasi refresh token (sesi lama direvoke).
func (s *Service) Refresh(ctx context.Context, orgSlug, rawRefresh, ip, ua string) (*TokenPair, error) {
	orgID, _, err := s.ResolveOrg(ctx, orgSlug)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(rawRefresh) == "" {
		return nil, apperr.Validation("refresh_token wajib")
	}
	now := s.now()
	var out *TokenPair
	err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var sid, cid uuid.UUID
		var exp time.Time
		var revoked *time.Time
		var device *string
		var status string
		if err := tx.QueryRow(ctx, `SELECT se.id, se.customer_id, se.expires_at, se.revoked_at, se.device_id, c.status FROM bvrooms_sessions se JOIN bvrooms_customers c ON c.id = se.customer_id WHERE se.refresh_hash = $1`, iam.HashToken(rawRefresh)).Scan(&sid, &cid, &exp, &revoked, &device, &status); err != nil {
			return apperr.Unauthorized("refresh token tidak valid")
		}
		if revoked != nil || now.After(exp) {
			return apperr.Unauthorized("refresh token kedaluwarsa")
		}
		if status != "active" {
			return apperr.Forbidden("Akun diblokir")
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_sessions SET revoked_at = now() WHERE id = $1`, sid); err != nil {
			return err
		}
		var err error
		out, err = s.issueSessionTx(ctx, tx, orgID, cid, deref(device), ip, ua, now)
		return err
	})
	return out, err
}

// Logout: revoke sesi aktif + push subscription perangkat.
func (s *Service) Logout(ctx context.Context) error {
	p, err := customer(ctx)
	if err != nil {
		return err
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE bvrooms_sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, p.SessionID)
		return err
	})
}

// ---------- customer ----------

const customerSelect = `SELECT id, full_name, phone_e164, email, status, locale, created_at, last_login_at FROM bvrooms_customers`

func scanCustomer(row pgx.Row) (*Customer, error) {
	var c Customer
	if err := row.Scan(&c.ID, &c.FullName, &c.Phone, &c.Email, &c.Status, &c.Locale, &c.CreatedAt, &c.LastLogin); err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Service) customerByPhoneTx(ctx context.Context, tx pgx.Tx, phone string) (*Customer, error) {
	c, err := scanCustomer(tx.QueryRow(ctx, customerSelect+` WHERE phone_e164 = $1`, phone))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.New(404, "PHONE_NOT_REGISTERED", "Phone not registered", "Nomor belum terdaftar")
		}
		return nil, err
	}
	return c, nil
}

func (s *Service) customerByIDTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Customer, error) {
	c, err := scanCustomer(tx.QueryRow(ctx, customerSelect+` WHERE id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Customer")
		}
		return nil, err
	}
	return c, nil
}

func (s *Service) Me(ctx context.Context) (*Customer, error) {
	p, err := customer(ctx)
	if err != nil {
		return nil, err
	}
	var out *Customer
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		out, err = s.customerByIDTx(ctx, tx, p.UserID)
		return err
	})
	return out, err
}

type UpdateMeInput struct {
	FullName *string `json:"full_name"`
	Email    *string `json:"email"`
	Phone    *string `json:"phone"` // ganti nomor → otp_token purpose=change_phone untuk nomor baru, atau pin (mode PIN)
	OTPToken *string `json:"otp_token"`
	PIN      *string `json:"pin"`
	Locale   *string `json:"locale"`
}

// UpdateMe: Edit Data Akun (Figma AKUN). Ganti nomor HP wajib otp_token yang sudah diverifikasi ke nomor baru (mode OTP)
// atau konfirmasi PIN akun (mode PIN).
func (s *Service) UpdateMe(ctx context.Context, in UpdateMeInput) (*Customer, error) {
	p, err := customer(ctx)
	if err != nil {
		return nil, err
	}
	var newPhone *string
	var verifyPIN string
	if in.Phone != nil && strings.TrimSpace(*in.Phone) != "" {
		ph, err := NormalizePhone(*in.Phone)
		if err != nil {
			return nil, err
		}
		switch {
		case in.OTPToken != nil:
			claims, err := s.signer.verify(*in.OTPToken, audOTP)
			if err != nil || claims.Purpose != "change_phone" || claims.Phone != ph || claims.Org != p.OrganizationID.String() {
				return nil, apperr.Unauthorized("otp_token tidak valid untuk nomor ini")
			}
		case in.PIN != nil:
			pin, err := validatePIN(*in.PIN, "pin")
			if err != nil {
				return nil, err
			}
			verifyPIN = pin
		default:
			return nil, apperr.Validation("pin (atau otp_token) wajib untuk mengganti nomor HP").WithField("pin", "wajib")
		}
		newPhone = &ph
	}
	if in.Email != nil {
		e := strings.ToLower(strings.TrimSpace(*in.Email))
		if !emailRe.MatchString(e) {
			return nil, apperr.Validation("email tidak valid").WithField("email", "tidak valid")
		}
		in.Email = &e
	}
	if in.FullName != nil && strings.TrimSpace(*in.FullName) == "" {
		return nil, apperr.Validation("Nama lengkap tidak boleh kosong!").WithField("full_name", "wajib")
	}
	if in.Locale != nil && *in.Locale != "id" && *in.Locale != "en" {
		return nil, apperr.Validation("locale harus id|en")
	}
	var out *Customer
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		cur, err := s.customerByIDTx(ctx, tx, p.UserID)
		if err != nil {
			return err
		}
		if newPhone != nil && *newPhone != cur.Phone {
			if verifyPIN != "" {
				if _, err := s.checkPINTx(ctx, tx, cur, verifyPIN, s.now()); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE bvrooms_customers SET phone_e164 = $2 WHERE id = $1`, p.UserID, *newPhone); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("PHONE_EXISTS", "Nomor sudah dipakai akun lain")
				}
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_customers SET full_name = COALESCE(NULLIF(TRIM($2),''), full_name), email = COALESCE($3, email), locale = COALESCE($4, locale) WHERE id = $1`,
			p.UserID, deref(in.FullName), in.Email, in.Locale); err != nil {
			return err
		}
		_ = audit.LogAs(ctx, tx, p.OrganizationID, nil, p.IP, p.UserAgent, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "bvrooms_customer", EntityID: &p.UserID, EntityLabel: cur.FullName, After: map[string]any{"phone_changed": newPhone != nil}})
		out, err = s.customerByIDTx(ctx, tx, p.UserID)
		return err
	})
	return out, err
}
