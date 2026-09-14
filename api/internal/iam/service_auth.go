// Package iam: users, roles, permissions, teams, sessions, device tokens (TAD §5.6–5.7).
package iam

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam/catalog"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
)

type Service struct {
	DB         *db.DB
	Signer     *TokenSigner
	RefreshTTL time.Duration
	Catalog    *catalog.Catalog

	permCache    sync.Map // userID -> cachedPrincipal
	loginLimiter *loginLimiter
}

func NewService(d *db.DB, signer *TokenSigner, refreshTTL time.Duration) *Service {
	return &Service{DB: d, Signer: signer, RefreshTTL: refreshTTL, Catalog: catalog.MustLoad(), loginLimiter: newLoginLimiter(5)}
}

// SetLoginRateLimit: percobaan login per menit per IP (0 = nonaktif; dipakai test).
func (s *Service) SetLoginRateLimit(perMinute int) { s.loginLimiter = newLoginLimiter(perMinute) }

type LoginInput struct {
	Identifier string // email atau username
	Password   string
	Client     string // web | mobile
	DeviceID   string
	IP         string
	UserAgent  string
}

type TokenPair struct {
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token,omitempty"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	TokenType        string    `json:"token_type"`
}

type userRow struct {
	ID           uuid.UUID
	OrgID        uuid.UUID
	FullName     string
	PasswordHash string
	IsActive     bool
	PermVersion  int
	FailedCount  int
	LockedUntil  *time.Time
}

// Login: argon2id verify, lockout progresif, buat session + token (TAD §5.6).
func (s *Service) Login(ctx context.Context, in LoginInput) (*TokenPair, *authctx.Principal, error) {
	in.Identifier = strings.TrimSpace(strings.ToLower(in.Identifier))
	if in.Identifier == "" || in.Password == "" {
		return nil, nil, apperr.Validation("identifier dan password wajib")
	}
	if !s.loginLimiter.Allow(in.IP) {
		return nil, nil, apperr.RateLimited()
	}
	if in.Client == "" {
		in.Client = "web"
	}
	var pair *TokenPair
	var principal *authctx.Principal
	err := s.DB.WithAuthLookupTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var u userRow
		// email unik global; username unik per organization → bila username cocok di >1 org, minta pakai email
		var matches int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE deleted_at IS NULL AND (lower(email) = $1 OR lower(username) = $1)`, in.Identifier).Scan(&matches)
		if matches > 1 {
			return apperr.Validation("Username dipakai di lebih dari satu organization; gunakan email untuk login")
		}
		err := tx.QueryRow(ctx, `
			SELECT id, organization_id, full_name, password_hash, is_active, permission_version, failed_login_count, locked_until
			FROM users WHERE deleted_at IS NULL AND (lower(email) = $1 OR lower(username) = $1) LIMIT 1`, in.Identifier).
			Scan(&u.ID, &u.OrgID, &u.FullName, &u.PasswordHash, &u.IsActive, &u.PermVersion, &u.FailedCount, &u.LockedUntil)
		if err != nil {
			if db.IsNoRows(err) {
				// dummy verify agar timing seragam
				VerifyPassword("$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", in.Password)
				return apperr.Unauthorized("Email/username atau password salah")
			}
			return err
		}
		if !u.IsActive {
			return apperr.Unauthorized("Akun tidak aktif")
		}
		if u.LockedUntil != nil && u.LockedUntil.After(time.Now()) {
			return apperr.New(423, "ACCOUNT_LOCKED", "Account locked", "Akun terkunci sementara; coba lagi nanti")
		}
		if !VerifyPassword(u.PasswordHash, in.Password) {
			failed := u.FailedCount + 1
			var lock *time.Time
			if failed >= 5 {
				// lockout progresif: 1, 2, 4, 8... menit (maks 60)
				mins := 1 << min(failed-5, 6)
				if mins > 60 {
					mins = 60
				}
				t := time.Now().Add(time.Duration(mins) * time.Minute)
				lock = &t
			}
			_ = s.DB.WithAuthLookupTx(ctx, func(ctx context.Context, tx2 pgx.Tx) error {
				_, _ = tx2.Exec(ctx, `UPDATE users SET failed_login_count = $2, locked_until = $3 WHERE id = $1`, u.ID, failed, lock)
				return audit.LogAs(ctx, tx2, u.OrgID, &u.ID, in.IP, in.UserAgent, audit.AuditEntry{Action: audit.AuditLoginFailed, EntityType: "user", EntityID: &u.ID, EntityLabel: u.FullName})
			})
			return apperr.Unauthorized("Email/username atau password salah")
		}
		_, err = tx.Exec(ctx, `UPDATE users SET failed_login_count = 0, locked_until = NULL, last_login_at = now() WHERE id = $1`, u.ID)
		if err != nil {
			return err
		}
		// buat session
		raw, hash, err := NewRefreshToken()
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		var sid uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO sessions (organization_id, user_id, refresh_token_hash, client, device_id, ip, user_agent, expires_at)
			VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,'')::inet,NULLIF($7,''),$8) RETURNING id`,
			u.OrgID, u.ID, hash, in.Client, in.DeviceID, in.IP, in.UserAgent, now.Add(s.RefreshTTL)).Scan(&sid)
		if err != nil {
			return err
		}
		access, exp, err := s.Signer.Sign(u.ID, u.OrgID, sid, u.PermVersion, in.Client, now)
		if err != nil {
			return err
		}
		pair = &TokenPair{AccessToken: access, AccessExpiresAt: exp, RefreshToken: raw, RefreshExpiresAt: now.Add(s.RefreshTTL), TokenType: "Bearer"}
		_ = audit.LogAs(ctx, tx, u.OrgID, &u.ID, in.IP, in.UserAgent, audit.AuditEntry{Action: audit.AuditLogin, EntityType: "user", EntityID: &u.ID, EntityLabel: u.FullName, After: map[string]any{"client": in.Client}})
		principal, err = s.loadPrincipalTx(ctx, tx, u.ID, u.OrgID, sid, u.PermVersion)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	s.permCache.Delete(principal.UserID)
	return pair, principal, nil
}

// Refresh: rotasi refresh token; reuse-detection → revoke seluruh session user (TAD §5.6).
func (s *Service) Refresh(ctx context.Context, rawRefresh, client, ip, ua string) (*TokenPair, error) {
	if rawRefresh == "" {
		return nil, apperr.Unauthorized("refresh token wajib")
	}
	hash := HashToken(rawRefresh)
	var pair *TokenPair
	err := s.DB.WithAuthLookupTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var sid, userID, orgID uuid.UUID
		var expires time.Time
		var revoked *time.Time
		var prevHash *string
		err := tx.QueryRow(ctx, `SELECT id, user_id, organization_id, expires_at, revoked_at, previous_token_hash FROM sessions WHERE refresh_token_hash = $1`, hash).
			Scan(&sid, &userID, &orgID, &expires, &revoked, &prevHash)
		if err != nil {
			if db.IsNoRows(err) {
				// mungkin token lama yang sudah dirotasi → reuse detection
				var reuseUser uuid.UUID
				var reuseOrg uuid.UUID
				if e2 := tx.QueryRow(ctx, `SELECT user_id, organization_id FROM sessions WHERE previous_token_hash = $1`, hash).Scan(&reuseUser, &reuseOrg); e2 == nil {
					// revoke di transaksi terpisah agar tetap commit meski handler mengembalikan 401
					_ = s.DB.WithAuthLookupTx(ctx, func(ctx context.Context, tx2 pgx.Tx) error {
						_, _ = tx2.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, reuseUser)
						return audit.LogAs(ctx, tx2, reuseOrg, &reuseUser, ip, ua, audit.AuditEntry{Action: audit.AuditTokenReuse, EntityType: "session", EntityID: &sid})
					})
					s.permCache.Delete(reuseUser)
					return apperr.Unauthorized("Sesi tidak valid; silakan login ulang")
				}
				return apperr.Unauthorized("Sesi tidak ditemukan")
			}
			return err
		}
		if revoked != nil || expires.Before(time.Now()) {
			return apperr.Unauthorized("Sesi berakhir; silakan login ulang")
		}
		var isActive bool
		var permVer int
		if err := tx.QueryRow(ctx, `SELECT is_active, permission_version FROM users WHERE id = $1 AND deleted_at IS NULL`, userID).Scan(&isActive, &permVer); err != nil || !isActive {
			return apperr.Unauthorized("Akun tidak aktif")
		}
		newRaw, newHash, err := NewRefreshToken()
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `UPDATE sessions SET refresh_token_hash = $2, previous_token_hash = $3, last_used_at = now(), expires_at = $4 WHERE id = $1`,
			sid, newHash, hash, now.Add(s.RefreshTTL)); err != nil {
			return err
		}
		if client == "" {
			client = "web"
		}
		access, exp, err := s.Signer.Sign(userID, orgID, sid, permVer, client, now)
		if err != nil {
			return err
		}
		pair = &TokenPair{AccessToken: access, AccessExpiresAt: exp, RefreshToken: newRaw, RefreshExpiresAt: now.Add(s.RefreshTTL), TokenType: "Bearer"}
		return nil
	})
	return pair, err
}

func (s *Service) Logout(ctx context.Context, rawRefresh string) error {
	p, ok := authctx.From(ctx)
	return s.DB.WithAuthLookupTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if rawRefresh != "" {
			_, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE refresh_token_hash = $1 AND revoked_at IS NULL`, HashToken(rawRefresh))
			if err != nil {
				return err
			}
		}
		if ok && p.SessionID != uuid.Nil {
			_, _ = tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, p.SessionID)
			_, _ = tx.Exec(ctx, `DELETE FROM device_tokens WHERE user_id = $1 AND device_id = (SELECT device_id FROM sessions WHERE id = $2)`, p.UserID, p.SessionID)
			_ = audit.LogAs(ctx, tx, p.OrganizationID, &p.UserID, p.IP, p.UserAgent, audit.AuditEntry{Action: audit.AuditLogout, EntityType: "user", EntityID: &p.UserID})
			s.permCache.Delete(p.UserID)
		}
		return nil
	})
}

// ---------- Principal ----------

type cachedPrincipal struct {
	p       *authctx.Principal
	loaded  time.Time
	version int
}

// PrincipalFromClaims memuat principal (permission set per property, team) — cache 60 detik, invalidasi via ver.
func (s *Service) PrincipalFromClaims(ctx context.Context, c *Claims) (*authctx.Principal, error) {
	userID, err := uuid.Parse(c.Subject)
	if err != nil {
		return nil, apperr.Unauthorized("token tidak valid")
	}
	orgID, err := uuid.Parse(c.Org)
	if err != nil {
		return nil, apperr.Unauthorized("token tidak valid")
	}
	sid, _ := uuid.Parse(c.Sid)
	if v, ok := s.permCache.Load(userID); ok {
		cp := v.(cachedPrincipal)
		if cp.version == c.Ver && time.Since(cp.loaded) < 60*time.Second {
			cl := *cp.p
			cl.SessionID = sid
			return &cl, nil
		}
	}
	var p *authctx.Principal
	err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var isActive bool
		var ver int
		var sessRevoked *time.Time
		if err := tx.QueryRow(ctx, `SELECT is_active, permission_version FROM users WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL`, userID, orgID).Scan(&isActive, &ver); err != nil {
			return apperr.Unauthorized("akun tidak ditemukan")
		}
		if !isActive {
			return apperr.Unauthorized("akun tidak aktif")
		}
		if ver != c.Ver {
			return apperr.New(401, "TOKEN_STALE", "Token stale", "Permission berubah; refresh token diperlukan")
		}
		if sid != uuid.Nil {
			if err := tx.QueryRow(ctx, `SELECT revoked_at FROM sessions WHERE id = $1`, sid).Scan(&sessRevoked); err == nil && sessRevoked != nil {
				return apperr.Unauthorized("sesi telah berakhir")
			}
		}
		var perr error
		p, perr = s.loadPrincipalTx(ctx, tx, userID, orgID, sid, ver)
		return perr
	})
	if err != nil {
		return nil, err
	}
	s.permCache.Store(userID, cachedPrincipal{p: p, loaded: time.Now(), version: c.Ver})
	return p, nil
}

func (s *Service) InvalidateUser(userID uuid.UUID) { s.permCache.Delete(userID) }

func (s *Service) loadPrincipalTx(ctx context.Context, tx pgx.Tx, userID, orgID, sid uuid.UUID, ver int) (*authctx.Principal, error) {
	p := &authctx.Principal{UserID: userID, OrganizationID: orgID, SessionID: sid, PermissionVersion: ver}
	if err := tx.QueryRow(ctx, `SELECT full_name FROM users WHERE id = $1`, userID).Scan(&p.FullName); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT r.code, ur.property_id, array_agg(rp.permission_code ORDER BY rp.permission_code)
		FROM user_roles ur JOIN roles r ON r.id = ur.role_id AND r.deleted_at IS NULL
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		WHERE ur.user_id = $1 GROUP BY r.code, ur.property_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roleSet := map[string]struct{}{}
	for rows.Next() {
		var code string
		var propID *uuid.UUID
		var perms []*string
		if err := rows.Scan(&code, &propID, &perms); err != nil {
			return nil, err
		}
		roleSet[code] = struct{}{}
		g := authctx.PropertyGrant{PropertyID: propID, Permissions: map[string]struct{}{}}
		for _, pc := range perms {
			if pc != nil {
				g.Permissions[*pc] = struct{}{}
			}
		}
		p.Grants = append(p.Grants, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for r := range roleSet {
		p.RoleCodes = append(p.RoleCodes, r)
	}
	trows, err := tx.Query(ctx, `SELECT team_id, is_lead FROM team_members WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		var tid uuid.UUID
		var lead bool
		if err := trows.Scan(&tid, &lead); err != nil {
			return nil, err
		}
		p.TeamIDs = append(p.TeamIDs, tid)
		if lead {
			p.LeadTeamIDs = append(p.LeadTeamIDs, tid)
		}
	}
	return p, trows.Err()
}

// ---------- Login rate limiter (5/menit/IP) ----------

type loginLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
	max  int
}

func newLoginLimiter(max int) *loginLimiter {
	return &loginLimiter{hits: map[string][]time.Time{}, max: max}
}

func (l *loginLimiter) Allow(ip string) bool {
	if ip == "" || l.max <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cut := now.Add(-time.Minute)
	var keep []time.Time
	for _, t := range l.hits[ip] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	if len(keep) >= l.max {
		l.hits[ip] = keep
		return false
	}
	l.hits[ip] = append(keep, now)
	if len(l.hits) > 10000 { // reset kasar agar memori terbatas
		l.hits = map[string][]time.Time{}
	}
	return true
}

var ErrNotFound = errors.New("not found")

func (s *Service) invalidateAll() {
	s.permCache.Range(func(k, _ any) bool { s.permCache.Delete(k); return true })
}
