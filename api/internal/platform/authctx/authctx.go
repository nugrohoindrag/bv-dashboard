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
type PropertyGrant struct {
	PropertyID  *uuid.UUID
	Permissions map[string]struct{}
}

type Principal struct {
	UserID            uuid.UUID
	OrganizationID    uuid.UUID
	SessionID         uuid.UUID
	PermissionVersion int
	FullName          string
	RoleCodes         []string
	TeamIDs           []uuid.UUID
	LeadTeamIDs       []uuid.UUID
	Grants            []PropertyGrant
	Source            Source
	IsTenant          bool // akun Mobile Tenant (role tenant_user/tenant_admin) — tidak pernah punya permission staf
	IsInternalAdmin   bool // role admin_internal pada organization is_internal (Website PRD §18) — dicek server-side
	RequestID         string
	IP                string
	UserAgent         string
	IsSystem          bool
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

// HasOnProperty: permission pada property tertentu (scope PRD §18.2).
func (p *Principal) HasOnProperty(perm string, propertyID uuid.UUID) bool {
	if p.IsSystem {
		return true
	}
	for _, g := range p.Grants {
		if g.PropertyID != nil && *g.PropertyID != propertyID {
			continue
		}
		if matchPerm(g.Permissions, perm) {
			return true
		}
	}
	return false
}

// PropertyIDsFor: daftar property tempat user memiliki perm; (nil, true) bila seluruh org.
func (p *Principal) PropertyIDsFor(perm string) (ids []uuid.UUID, all bool) {
	if p.IsSystem {
		return nil, true
	}
	seen := map[uuid.UUID]struct{}{}
	for _, g := range p.Grants {
		if !matchPerm(g.Permissions, perm) {
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
