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
	"github.com/buildingvision/api/internal/platform/cache"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/mailer"
)

type Service struct {
	DB         *db.DB
	Signer     *TokenSigner
	RefreshTTL time.Duration
	Catalog    *catalog.Catalog

	// Undangan user (PRD P0 v2 §26.1)
	Mailer           mailer.Mailer
	PublicURL        string
	ExposeInviteLink bool // non-produksi tanpa SMTP: kembalikan tautan undangan di respons

	permOnce     sync.Once
	permCache    *cache.TTLMap[uuid.UUID, cachedPrincipal] // userID -> principal (TTL 60 s, disapu otomatis)
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
	// MustChangePassword: akun dengan password sementara wajib mengganti password setelah login (PRD P3 v2.1 P3-ACC-03).
	MustChangePassword bool `json:"must_change_password,omitempty"`
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
		if u.LockedUntil != nil && u.LockedUntil.After(time.Now()) {
			s.auditLoginFailed(ctx, u, in, "account_locked")
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
				return audit.LogAs(ctx, tx2, u.OrgID, &u.ID, in.IP, in.UserAgent, audit.AuditEntry{Action: audit.AuditLoginFailed, EntityType: "user", EntityID: &u.ID, EntityLabel: u.FullName, After: map[string]any{"reason": "invalid_password", "failed_count": failed}})
			})
			return apperr.Unauthorized("Email/username atau password salah")
		}
		// PRD P0 v2 §6: organization nonaktif/ditangguhkan tidak dapat login (dicek setelah kredensial valid)
		if err := orgStatusError(ctx, tx, u.OrgID); err != nil {
			s.auditLoginFailed(ctx, u, in, "organization_"+err.Code)
			return err
		}
		// TD-P1-003: akun Mobile Tenant vs akun staf — dicek setelah password valid agar status tidak bocor tanpa kredensial
		var tuStatus *string
		_ = tx.QueryRow(ctx, `SELECT status FROM tenant_users WHERE user_id = $1`, u.ID).Scan(&tuStatus)
		// kredensial sudah valid → konteks org agar data property (nama, nomor WhatsApp pengelola) terbaca untuk pesan status akun
		if _, err := tx.Exec(ctx, `SELECT set_config('app.organization_id', $1, true)`, u.OrgID.String()); err != nil {
			return err
		}
		isTenant := tuStatus != nil
		if in.Client == authctx.ClientTenantApp && !isTenant {
			s.auditLoginFailed(ctx, u, in, "not_tenant_account")
			return apperr.New(403, "NOT_TENANT_ACCOUNT", "Not a tenant account", "Akun ini bukan akun tenant; gunakan dashboard atau Staff App")
		}
		if in.Client != authctx.ClientTenantApp && isTenant {
			s.auditLoginFailed(ctx, u, in, "tenant_account_only")
			return apperr.New(403, "TENANT_ACCOUNT_ONLY", "Tenant account", "Akun tenant hanya dapat masuk melalui Tenant App")
		}
		if isTenant && *tuStatus != "active" {
			s.auditLoginFailed(ctx, u, in, "tenant_"+*tuStatus)
		}
		if isTenant && *tuStatus != "active" {
			// PRD P3 v2.1 P3-ACC-07 / B-08: keputusan akun & alasannya tampil di layar login (notifikasi in-app tidak terbaca
			// karena akun nonaktif), beserta kontak WhatsApp pengelola property (P3-WAM-05).
			var reason, propName, wa *string
			_ = tx.QueryRow(ctx, `SELECT CASE tu.status WHEN 'rejected' THEN tu.rejection_reason WHEN 'suspended' THEN tu.suspension_reason END, l.name, p.whatsapp_number
				FROM tenant_users tu JOIN properties p ON p.location_id = tu.property_id JOIN locations l ON l.id = tu.property_id WHERE tu.user_id = $1`, u.ID).Scan(&reason, &propName, &wa)
			var e *apperr.Error
			switch *tuStatus {
			case "pending_validation":
				e = apperr.New(403, "ACCOUNT_PENDING", "Account pending validation", "Akun menunggu validasi building management")
			case "rejected":
				e = apperr.New(403, "ACCOUNT_REJECTED", "Account rejected", "Pendaftaran akun ditolak")
			case "suspended":
				e = apperr.New(403, "ACCOUNT_SUSPENDED", "Account suspended", "Akun ditangguhkan; hubungi building management")
			}
			if e != nil {
				e = e.WithMeta("account_status", *tuStatus)
				if reason != nil && *reason != "" {
					e = e.WithMeta("reason", *reason)
				}
				if propName != nil {
					e = e.WithMeta("property_name", *propName)
				}
				if wa != nil && *wa != "" {
					e = e.WithMeta("whatsapp_number", *wa)
				}
				return e
			}
		}
		if !u.IsActive {
			s.auditLoginFailed(ctx, u, in, "user_inactive")
			return apperr.Unauthorized("Akun tidak aktif")
		}
		_, err = tx.Exec(ctx, `UPDATE users SET failed_login_count = 0, locked_until = NULL, last_login_at = now() WHERE id = $1`, u.ID)
		if err != nil {
			return err
		}
		if isTenant {
			_, _ = tx.Exec(ctx, `UPDATE tenant_users SET last_seen_at = now() WHERE user_id = $1`, u.ID)
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
		_ = tx.QueryRow(ctx, `SELECT must_change_password FROM users WHERE id = $1`, u.ID).Scan(&pair.MustChangePassword)
		_ = audit.LogAs(ctx, tx, u.OrgID, &u.ID, in.IP, in.UserAgent, audit.AuditEntry{Action: audit.AuditLogin, EntityType: "user", EntityID: &u.ID, EntityLabel: u.FullName, After: map[string]any{"client": in.Client}})
		// scope org agar roles/teams (RLS) terbaca untuk respons login (user.roles, is_internal_admin)
		if _, err := tx.Exec(ctx, `SELECT set_config('app.organization_id', $1, true)`, u.OrgID.String()); err != nil {
			return err
		}
		principal, err = s.loadPrincipalTx(ctx, tx, u.ID, u.OrgID, sid, u.PermVersion)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	s.principalCache().Delete(principal.UserID)
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
		// FOR UPDATE: refresh paralel dengan token yang sama diserialisasi (yang kedua melihat token sudah dirotasi)
		err := tx.QueryRow(ctx, `SELECT id, user_id, organization_id, expires_at, revoked_at, previous_token_hash FROM sessions WHERE refresh_token_hash = $1 FOR UPDATE`, hash).
			Scan(&sid, &userID, &orgID, &expires, &revoked, &prevHash)
		if err != nil {
			if db.IsNoRows(err) {
				// mungkin token lama yang sudah dirotasi → reuse detection
				var reuseUser uuid.UUID
				var reuseOrg uuid.UUID
				var rotatedAt *time.Time
				if e2 := tx.QueryRow(ctx, `SELECT user_id, organization_id, last_used_at FROM sessions WHERE previous_token_hash = $1 AND revoked_at IS NULL`, hash).Scan(&reuseUser, &reuseOrg, &rotatedAt); e2 == nil {
					// grace: token baru saja dirotasi oleh request paralel yang sah (multi-tab / efek ganda) → tolak tanpa
					// mencabut sesi; klien memakai cookie hasil rotasi. Di luar jendela ini tetap dianggap pencurian token.
					if rotatedAt != nil && time.Since(*rotatedAt) < refreshReuseGrace {
						return apperr.New(401, "REFRESH_SUPERSEDED", "Refresh superseded", "Token sudah diperbarui oleh permintaan lain; ulangi")
					}
					// revoke di transaksi terpisah agar tetap commit meski handler mengembalikan 401
					_ = s.DB.WithAuthLookupTx(ctx, func(ctx context.Context, tx2 pgx.Tx) error {
						_, _ = tx2.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, reuseUser)
						return audit.LogAs(ctx, tx2, reuseOrg, &reuseUser, ip, ua, audit.AuditEntry{Action: audit.AuditTokenReuse, EntityType: "session", EntityID: &sid})
					})
					s.principalCache().Delete(reuseUser)
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
		if err := orgStatusError(ctx, tx, orgID); err != nil {
			return err
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
			var sid, uid, oid uuid.UUID
			err := tx.QueryRow(ctx, `UPDATE sessions SET revoked_at = now() WHERE refresh_token_hash = $1 AND revoked_at IS NULL RETURNING id, user_id, organization_id`, HashToken(rawRefresh)).Scan(&sid, &uid, &oid)
			if err != nil && !db.IsNoRows(err) {
				return err
			}
			// logout tanpa access token (hanya cookie/refresh token) tetap tercatat (PRD P0 v2 §16)
			if err == nil && (!ok || p.SessionID != sid) {
				ip, ua := "", ""
				if ok {
					ip, ua = p.IP, p.UserAgent
				}
				_ = audit.LogAs(ctx, tx, oid, &uid, ip, ua, audit.AuditEntry{Action: audit.AuditLogout, EntityType: "user", EntityID: &uid, After: map[string]any{"session_id": sid}})
				s.principalCache().Delete(uid)
			}
		}
		if ok && p.SessionID != uuid.Nil {
			_, _ = tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, p.SessionID)
			_, _ = tx.Exec(ctx, `DELETE FROM device_tokens WHERE user_id = $1 AND device_id = (SELECT device_id FROM sessions WHERE id = $2)`, p.UserID, p.SessionID)
			_ = audit.LogAs(ctx, tx, p.OrganizationID, &p.UserID, p.IP, p.UserAgent, audit.AuditEntry{Action: audit.AuditLogout, EntityType: "user", EntityID: &p.UserID})
			s.principalCache().Delete(p.UserID)
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
	if cp, ok := s.principalCache().Get(userID); ok && cp.version == c.Ver {
		cl := *cp.p
		cl.SessionID = sid
		return &cl, nil
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
		if err := orgStatusError(ctx, tx, orgID); err != nil {
			return err
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
	s.principalCache().Set(userID, cachedPrincipal{p: p, loaded: time.Now(), version: c.Ver})
	return p, nil
}

const principalCacheTTL = 60 * time.Second

// refreshReuseGrace: jendela toleransi refresh paralel (lihat Refresh).
const refreshReuseGrace = 20 * time.Second

// principalCache: lazy agar Service yang dibuat literal (test) tetap punya cache.
func (s *Service) principalCache() *cache.TTLMap[uuid.UUID, cachedPrincipal] {
	s.permOnce.Do(func() { s.permCache = cache.New[uuid.UUID, cachedPrincipal](principalCacheTTL) })
	return s.permCache
}

func (s *Service) InvalidateUser(userID uuid.UUID) { s.principalCache().Delete(userID) }

func (s *Service) loadPrincipalTx(ctx context.Context, tx pgx.Tx, userID, orgID, sid uuid.UUID, ver int) (*authctx.Principal, error) {
	p := &authctx.Principal{UserID: userID, OrganizationID: orgID, SessionID: sid, PermissionVersion: ver}
	if err := tx.QueryRow(ctx, `SELECT full_name, vendor_id, must_change_password FROM users WHERE id = $1`, userID).Scan(&p.FullName, &p.VendorID, &p.MustChangePassword); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT r.code, ur.property_id, ur.scope_location_id, COALESCE(sl.path::text, ''), array_agg(rp.permission_code ORDER BY rp.permission_code)
		FROM user_roles ur JOIN roles r ON r.id = ur.role_id AND r.deleted_at IS NULL
		LEFT JOIN locations sl ON sl.id = ur.scope_location_id
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		WHERE ur.user_id = $1 GROUP BY r.code, ur.property_id, ur.scope_location_id, sl.path`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roleSet := map[string]struct{}{}
	for rows.Next() {
		var code, scopePath string
		var propID, scopeID *uuid.UUID
		var perms []*string
		if err := rows.Scan(&code, &propID, &scopeID, &scopePath, &perms); err != nil {
			return nil, err
		}
		roleSet[code] = struct{}{}
		g := authctx.PropertyGrant{PropertyID: propID, ScopeLocationID: scopeID, ScopePath: scopePath, Permissions: map[string]struct{}{}}
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
		if catalog.IsTenantRole(r) {
			p.IsTenant = true
		}
		if catalog.IsInternalRole(r) {
			// role internal hanya berlaku bila organization memang internal (bukan sekadar nama role)
			var internal bool
			_ = tx.QueryRow(ctx, `SELECT is_internal FROM organizations WHERE id = $1`, orgID).Scan(&internal)
			p.IsInternalAdmin = p.IsInternalAdmin || internal
			if r == catalog.RolePlatformAdmin {
				p.IsPlatformAdmin = internal
			}
		}
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

// InvalidateAll: kosongkan cache principal instance ini (mis. status organization berubah). Instance lain
// mengikuti lewat permission_version yang dinaikkan di DB (token stale → refresh → cek status org).
func (s *Service) InvalidateAll() { s.invalidateAll() }

func (s *Service) invalidateAll() {
	s.permOnce.Do(func() {})
	s.permCache = cache.New[uuid.UUID, cachedPrincipal](principalCacheTTL)
}

// LoadPrincipal: principal penuh untuk user (dipakai worker export agar permission tetap berlaku).
func (s *Service) LoadPrincipal(ctx context.Context, userID, orgID uuid.UUID) (*authctx.Principal, error) {
	var p *authctx.Principal
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var ver int
		if err := tx.QueryRow(ctx, `SELECT permission_version FROM users WHERE id = $1 AND is_active AND deleted_at IS NULL`, userID).Scan(&ver); err != nil {
			return apperr.NotFound("User")
		}
		var err error
		p, err = s.loadPrincipalTx(ctx, tx, userID, orgID, uuid.Nil, ver)
		return err
	})
	if err != nil {
		return nil, err
	}
	p.Source = authctx.SourceSystem
	return p, nil
}

// IssueSessionTx: membuat session + token pair untuk user yang sudah terautentikasi lewat jalur lain
// (verifikasi email signup, Website PRD §25). Dipanggil di dalam transaksi org user tersebut.
func (s *Service) IssueSessionTx(ctx context.Context, tx pgx.Tx, userID, orgID uuid.UUID, client, ip, ua string) (*TokenPair, error) {
	if client == "" {
		client = "web"
	}
	var permVersion int
	var fullName string
	if err := tx.QueryRow(ctx, `SELECT permission_version, full_name FROM users WHERE id = $1 AND organization_id = $2 AND is_active AND deleted_at IS NULL`, userID, orgID).Scan(&permVersion, &fullName); err != nil {
		return nil, apperr.Unauthorized("akun tidak ditemukan")
	}
	raw, hash, err := NewRefreshToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var sid uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO sessions (organization_id, user_id, refresh_token_hash, client, ip, user_agent, expires_at)
		VALUES ($1,$2,$3,$4,NULLIF($5,'')::inet,NULLIF($6,''),$7) RETURNING id`,
		orgID, userID, hash, client, ip, ua, now.Add(s.RefreshTTL)).Scan(&sid); err != nil {
		return nil, err
	}
	_, _ = tx.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, userID)
	access, exp, err := s.Signer.Sign(userID, orgID, sid, permVersion, client, now)
	if err != nil {
		return nil, err
	}
	_ = audit.LogAs(ctx, tx, orgID, &userID, ip, ua, audit.AuditEntry{Action: audit.AuditLogin, EntityType: "user", EntityID: &userID, EntityLabel: fullName, After: map[string]any{"client": client, "via": "email_verification"}})
	s.principalCache().Delete(userID)
	return &TokenPair{AccessToken: access, AccessExpiresAt: exp, RefreshToken: raw, RefreshExpiresAt: now.Add(s.RefreshTTL), TokenType: "Bearer"}, nil
}
