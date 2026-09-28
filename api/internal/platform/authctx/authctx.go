// Package authctx menyimpan principal (user, organization, permission) di context.Context.
// Semua service membaca organization dari sini, bukan dari request (TAD §5.5).
package authctx

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

type Source string

const (
	SourceWeb       Source = "web"
	SourceMobile    Source = "mobile"
	SourceSystem    Source = "system"
	SourceSync      Source = "sync"
	SourceTenantApp Source = "tenant_app" // Mobile Tenant (P1)
	SourceBVRooms   Source = "bvrooms"    // customer BVRooms (bukan users; UserID = bvrooms_customers.id)
)

// ClientTenantApp: nilai `client` login/refresh untuk Mobile Tenant (TD-P1-003).
const ClientTenantApp = "tenant_app"

// PropertyGrant: permission set yang dimiliki user pada satu property (nil PropertyID = seluruh org).
// ScopeLocationID/ScopePath (PRD P0 v2 §8.4): grant hanya berlaku di subtree lokasi (building/tower) tersebut.
type PropertyGrant struct {
	PropertyID      *uuid.UUID
	ScopeLocationID *uuid.UUID
	ScopePath       string // ltree text lokasi scope
	Permissions     map[string]struct{}
}

// LocationScope: subtree lokasi tempat grant ber-scope building berlaku.
type LocationScope struct {
	PropertyID uuid.UUID
	LocationID uuid.UUID
	Path       string
}

type Principal struct {
	UserID             uuid.UUID
	OrganizationID     uuid.UUID
	SessionID          uuid.UUID
	PermissionVersion  int
	FullName           string
	RoleCodes          []string
	TeamIDs            []uuid.UUID
	LeadTeamIDs        []uuid.UUID
	Grants             []PropertyGrant
	Source             Source
	IsTenant           bool       // akun Mobile Tenant (role tenant_user/tenant_admin) — tidak pernah punya permission staf
	IsInternalAdmin    bool       // role admin_internal pada organization is_internal (Website PRD §18) — dicek server-side
	IsPlatformAdmin    bool       // role platform_admin pada organization is_internal (PRD P0 v2 §8.2)
	VendorID           *uuid.UUID // akun vendor (PRD P0 v2 §8.4 resource scope): hanya work order vendor ini
	MustChangePassword bool       // password sementara (PRD P3 v2.1 P3-ACC-03): tenant diblokir sampai ganti password
	RequestID          string
	IP                 string
	UserAgent          string
	IsSystem           bool
}

type ctxKey struct{}

func With(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func From(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(*Principal)
	return p, ok && p != nil
}

func Must(ctx context.Context) *Principal {
	p, ok := From(ctx)
	if !ok {
		panic("authctx: principal missing from context")
	}
	return p
}

// System membuat principal sistem (worker) untuk satu organization.
func System(orgID uuid.UUID) *Principal {
	return &Principal{OrganizationID: orgID, IsSystem: true, Source: SourceSystem, FullName: "System"}
}

// Has: permission di level organization (property mana pun).
func (p *Principal) Has(perm string) bool {
	if p.IsSystem {
		return true
	}
	for _, g := range p.Grants {
		if matchPerm(g.Permissions, perm) {
			return true
		}
	}
	return false
}

// HasOnProperty: permission pada SELURUH property tertentu (scope PRD §18.2). Grant ber-scope building tidak dihitung.
func (p *Principal) HasOnProperty(perm string, propertyID uuid.UUID) bool {
	if p.IsSystem {
		return true
	}
	for _, g := range p.Grants {
		if g.ScopeLocationID != nil {
			continue
		}
		if g.PropertyID != nil && *g.PropertyID != propertyID {
			continue
		}
		if matchPerm(g.Permissions, perm) {
			return true
		}
	}
	return false
}

// HasOnLocation: permission pada object di property + lokasi (ltree path) tertentu — property-wide grant atau
// grant ber-scope building/tower yang subtree-nya memuat path. path kosong = object tanpa lokasi.
func (p *Principal) HasOnLocation(perm string, propertyID uuid.UUID, path string) bool {
	if p.HasOnProperty(perm, propertyID) {
		return true
	}
	if path == "" {
		return false
	}
	for _, g := range p.Grants {
		if g.ScopeLocationID == nil || g.PropertyID == nil || *g.PropertyID != propertyID {
			continue
		}
		if (path == g.ScopePath || strings.HasPrefix(path, g.ScopePath+".")) && matchPerm(g.Permissions, perm) {
			return true
		}
	}
	return false
}

// HasOnPropertyAt: seperti HasOnLocation, tetapi path dimuat lazily (hanya bila user memiliki grant ber-scope).
func (p *Principal) HasOnPropertyAt(perm string, propertyID uuid.UUID, path func() string) bool {
	if p.HasOnProperty(perm, propertyID) {
		return true
	}
	if len(p.LocationScopesFor(perm)) == 0 || path == nil {
		return false
	}
	return p.HasOnLocation(perm, propertyID, path())
}

// HasAnyOnProperty: punya perm di property ini, baik property-wide maupun ber-scope building.
func (p *Principal) HasAnyOnProperty(perm string, propertyID uuid.UUID) bool {
	if p.HasOnProperty(perm, propertyID) {
		return true
	}
	for _, s := range p.LocationScopesFor(perm) {
		if s.PropertyID == propertyID {
			return true
		}
	}
	return false
}

// LocationScopesFor: subtree lokasi dari grant ber-scope building yang memuat perm.
func (p *Principal) LocationScopesFor(perm string) []LocationScope {
	var out []LocationScope
	for _, g := range p.Grants {
		if g.ScopeLocationID == nil || g.PropertyID == nil || !matchPerm(g.Permissions, perm) {
			continue
		}
		out = append(out, LocationScope{PropertyID: *g.PropertyID, LocationID: *g.ScopeLocationID, Path: g.ScopePath})
	}
	return out
}

// ScopeSQL: kondisi SQL (tanpa "AND") yang membatasi baris ke scope perm user — property-wide dan/atau subtree
// lokasi. propertyCol mis. "t.property_id"; pathExpr = ekspresi ltree lokasi object (boleh NULL → tidak lolos
// scope building). add menambahkan argumen dan mengembalikan placeholder.
func (p *Principal) ScopeSQL(perm, propertyCol, pathExpr string, add func(any) string) string {
	pids, all := p.PropertyIDsFor(perm)
	if all {
		return "TRUE"
	}
	var conds []string
	if len(pids) > 0 {
		conds = append(conds, propertyCol+" = ANY("+add(pids)+"::uuid[])")
	}
	if scopes := p.LocationScopesFor(perm); len(scopes) > 0 && pathExpr != "" {
		paths := make([]string, 0, len(scopes))
		for _, s := range scopes {
			paths = append(paths, s.Path)
		}
		conds = append(conds, "("+pathExpr+") <@ ANY("+add(paths)+"::ltree[])")
	}
	if len(conds) == 0 {
		return "FALSE"
	}
	return "(" + strings.Join(conds, " OR ") + ")"
}

// PropertyIDsFor: daftar property tempat user memiliki perm property-wide; (nil, true) bila seluruh org.
// Grant ber-scope building tidak disertakan — gunakan ScopeSQL untuk list yang sadar lokasi.
func (p *Principal) PropertyIDsFor(perm string) (ids []uuid.UUID, all bool) {
	if p.IsSystem {
		return nil, true
	}
	seen := map[uuid.UUID]struct{}{}
	for _, g := range p.Grants {
		if g.ScopeLocationID != nil || !matchPerm(g.Permissions, perm) {
			continue
		}
		if g.PropertyID == nil {
			return nil, true
		}
		if _, ok := seen[*g.PropertyID]; !ok {
			seen[*g.PropertyID] = struct{}{}
			ids = append(ids, *g.PropertyID)
		}
	}
	return ids, false
}

func (p *Principal) AllPermissions() []string {
	set := map[string]struct{}{}
	for _, g := range p.Grants {
		for k := range g.Permissions {
			set[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

func (p *Principal) IsMemberOfTeam(teamID uuid.UUID) bool {
	for _, t := range p.TeamIDs {
		if t == teamID {
			return true
		}
	}
	return false
}

func (p *Principal) IsLeadOfTeam(teamID uuid.UUID) bool {
	for _, t := range p.LeadTeamIDs {
		if t == teamID {
			return true
		}
	}
	return false
}

// matchPerm mendukung wildcard yang disimpan di set: "*", "module.*", "module.*.action", "module.object.*".
func matchPerm(set map[string]struct{}, perm string) bool {
	if _, ok := set[perm]; ok {
		return true
	}
	if _, ok := set["*"]; ok {
		return true
	}
	parts := strings.Split(perm, ".")
	if len(parts) != 3 {
		return false
	}
	if _, ok := set[parts[0]+".*"]; ok {
		return true
	}
	if _, ok := set[parts[0]+"."+parts[1]+".*"]; ok {
		return true
	}
	if _, ok := set[parts[0]+".*."+parts[2]]; ok {
		return true
	}
	return false
}

// MatchPermission diekspor untuk seed/expansi katalog.
func MatchPermission(pattern, perm string) bool {
	return matchPerm(map[string]struct{}{pattern: {}}, perm)
}
