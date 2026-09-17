package bvrooms

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/db"
)

// ---------- PIN (pengganti OTP selama vendor SMS di-hold, D3) ----------
//
// Login = nomor HP + PIN 4 digit. Akun lama (pin_hash NULL) memakai PIN default Cfg.DefaultPIN sampai customer menggantinya.
// 5 kali salah → kunci 15 menit. OTP tetap tersedia (auth/otp/*) untuk diaktifkan kembali lewat BV_BVROOMS_AUTH=otp.

const (
	pinMaxFailed  = 5
	pinLockPeriod = 15 * time.Minute
)

var pinRe = regexp.MustCompile(`^[0-9]{4}$`)

func validatePIN(pin, field string) (string, error) {
	pin = strings.TrimSpace(pin)
	if !pinRe.MatchString(pin) {
		return "", apperr.Validation("PIN harus 4 digit angka").WithField(field, "4 digit angka")
	}
	return pin, nil
}

type PINLoginInput struct {
	OrganizationSlug string `json:"organization_slug"`
	Phone            string `json:"phone"`
	PIN              string `json:"pin"`
	DeviceID         string `json:"device_id"`
}

type PINRegisterInput struct {
	OrganizationSlug string `json:"organization_slug"`
	Phone            string `json:"phone"`
	FullName         string `json:"full_name"`
	Email            string `json:"email"`
	PIN              string `json:"pin"`
	DeviceID         string `json:"device_id"`
}

type ChangePINInput struct {
	CurrentPIN string `json:"current_pin"`
	NewPIN     string `json:"new_pin"`
}

// checkPINTx: cocokkan PIN dengan pin_hash (atau PIN default bila NULL); hitung kegagalan & kunci sementara.
// Mengembalikan pinIsDefault=true bila akun masih memakai PIN default.
func (s *Service) checkPINTx(ctx context.Context, tx pgx.Tx, c *Customer, pin string, now time.Time) (bool, error) {
	var hash *string
	var failed int
	var lockedUntil *time.Time
	if err := tx.QueryRow(ctx, `SELECT pin_hash, pin_failed, pin_locked_until FROM bvrooms_customers WHERE id = $1 FOR UPDATE`, c.ID).Scan(&hash, &failed, &lockedUntil); err != nil {
		return false, err
	}
	if lockedUntil != nil && now.Before(*lockedUntil) {
		return false, apperr.New(423, "PIN_LOCKED", "PIN locked", "Terlalu banyak percobaan; coba lagi dalam 15 menit")
	}
	var ok bool
	if hash == nil {
		ok = s.Cfg.DefaultPIN != "" && pin == s.Cfg.DefaultPIN
	} else {
		ok = iam.VerifyPassword(*hash, pin)
	}
	if !ok {
		failed++
		var lock *time.Time
		if failed >= pinMaxFailed {
			t := now.Add(pinLockPeriod)
			lock, failed = &t, 0
		}
		_, _ = tx.Exec(ctx, `UPDATE bvrooms_customers SET pin_failed = $2, pin_locked_until = $3 WHERE id = $1`, c.ID, failed, lock)
		if lock != nil {
			return false, apperr.New(423, "PIN_LOCKED", "PIN locked", "Terlalu banyak percobaan; coba lagi dalam 15 menit")
		}
		return false, apperr.New(401, "PIN_INVALID", "PIN invalid", "PIN salah")
	}
	if failed != 0 || lockedUntil != nil {
		_, _ = tx.Exec(ctx, `UPDATE bvrooms_customers SET pin_failed = 0, pin_locked_until = NULL WHERE id = $1`, c.ID)
	}
	return hash == nil, nil
}

// PINLogin: nomor HP + PIN → token sesi. 404 PHONE_NOT_REGISTERED bila nomor belum terdaftar (Figma "nomor tidak terdaftar").
func (s *Service) PINLogin(ctx context.Context, in PINLoginInput, ip, ua string) (*AuthResult, error) {
	phone, err := NormalizePhone(in.Phone)
	if err != nil {
		return nil, err
	}
	pin, err := validatePIN(in.PIN, "pin")
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
	now := s.now()
	var out *AuthResult
	err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		c, err := s.customerByPhoneTx(ctx, tx, phone)
		if err != nil {
			return err
		}
		if c.Status != "active" {
			return apperr.Forbidden("Akun diblokir")
		}
		isDefault, err := s.checkPINTx(ctx, tx, c, pin, now)
		if err != nil {
			return err
		}
		pair, err := s.issueSessionTx(ctx, tx, orgID, c.ID, in.DeviceID, ip, ua, now)
		if err != nil {
			return err
		}
		_, _ = tx.Exec(ctx, `UPDATE bvrooms_customers SET last_login_at = now() WHERE id = $1`, c.ID)
		out = &AuthResult{TokenPair: pair, Customer: c, PINIsDefault: isDefault}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PINRegister: daftar langsung (nama, email, nomor HP, PIN) tanpa OTP → sesi.
func (s *Service) PINRegister(ctx context.Context, in PINRegisterInput, ip, ua string) (*AuthResult, error) {
	phone, err := NormalizePhone(in.Phone)
	if err != nil {
		return nil, err
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
	pin, err := validatePIN(in.PIN, "pin")
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
	hash, err := iam.HashPassword(pin)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var out *AuthResult
	err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO bvrooms_customers (organization_id, phone_e164, full_name, email, pin_hash, last_login_at) VALUES ($1,$2,$3,$4,$5,now()) RETURNING id`, orgID, phone, name, email, hash).Scan(&id); err != nil {
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
		_ = audit.LogAs(ctx, tx, orgID, nil, ip, ua, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "bvrooms_customer", EntityID: &id, EntityLabel: MaskPhone(phone), After: map[string]any{"source": "bvrooms_app", "auth": "pin"}})
		out = &AuthResult{TokenPair: pair, Customer: c}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ChangePIN: customer login mengganti PIN (PIN lama = PIN default bila belum pernah diganti).
func (s *Service) ChangePIN(ctx context.Context, in ChangePINInput) error {
	p, err := customer(ctx)
	if err != nil {
		return err
	}
	cur, err := validatePIN(in.CurrentPIN, "current_pin")
	if err != nil {
		return err
	}
	next, err := validatePIN(in.NewPIN, "new_pin")
	if err != nil {
		return err
	}
	if cur == next {
		return apperr.Validation("PIN baru harus berbeda dari PIN lama").WithField("new_pin", "sama dengan PIN lama")
	}
	hash, err := iam.HashPassword(next)
	if err != nil {
		return err
	}
	now := s.now()
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		c, err := s.customerByIDTx(ctx, tx, p.UserID)
		if err != nil {
			return err
		}
		if _, err := s.checkPINTx(ctx, tx, c, cur, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_customers SET pin_hash = $2 WHERE id = $1`, c.ID, hash); err != nil {
			return err
		}
		_ = audit.LogAs(ctx, tx, p.OrganizationID, nil, p.IP, p.UserAgent, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "bvrooms_customer", EntityID: &c.ID, EntityLabel: c.FullName, After: map[string]any{"pin_changed": true}})
		return nil
	})
}
