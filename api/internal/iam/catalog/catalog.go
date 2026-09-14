// Package catalog memuat katalog permission & role template dari permissions.yaml (TAD §5.7).
// File ini adalah salinan contracts/permissions.yaml; CI memastikan keduanya identik.
package catalog

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed permissions.yaml
var raw []byte

type Permission struct {
	Code   string
	Module string
	Object string
	Action string
}

type RoleTemplate struct {
	Code        string
	Label       string
	Domain      string
	Patterns    []string
	Permissions []string // expanded
}

type Catalog struct {
	Permissions []Permission
	Roles       []RoleTemplate
	byCode      map[string]Permission
}

type file struct {
	Version int                            `yaml:"version"`
	Modules map[string]map[string][]string `yaml:"modules"`
	Roles   map[string]struct {
		Label       string   `yaml:"label"`
		Permissions []string `yaml:"permissions"`
	} `yaml:"roles"`
}

var loaded *Catalog

// Load mem-parse YAML sekali dan meng-expand wildcard role.
func Load() (*Catalog, error) {
	if loaded != nil {
		return loaded, nil
	}
	var f file
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse permissions.yaml: %w", err)
	}
	c := &Catalog{byCode: map[string]Permission{}}
	for mod, objs := range f.Modules {
		for obj, actions := range objs {
			for _, act := range actions {
				p := Permission{Code: mod + "." + obj + "." + act, Module: mod, Object: obj, Action: act}
				c.Permissions = append(c.Permissions, p)
				c.byCode[p.Code] = p
			}
		}
	}
	sort.Slice(c.Permissions, func(i, j int) bool { return c.Permissions[i].Code < c.Permissions[j].Code })
	for code, r := range f.Roles {
		rt := RoleTemplate{Code: code, Label: r.Label, Patterns: r.Permissions, Domain: domainOf(code)}
		rt.Permissions = c.Expand(r.Permissions)
		c.Roles = append(c.Roles, rt)
	}
	sort.Slice(c.Roles, func(i, j int) bool { return c.Roles[i].Code < c.Roles[j].Code })
	loaded = c
	return c, nil
}

func MustLoad() *Catalog {
	c, err := Load()
	if err != nil {
		panic(err)
	}
	return c
}

func (c *Catalog) Exists(code string) bool { _, ok := c.byCode[code]; return ok }

// Expand mengubah pattern ("*", "module.*", "module.*.action", "module.object.*") ke daftar kode eksplisit.
func (c *Catalog) Expand(patterns []string) []string {
	set := map[string]struct{}{}
	for _, pat := range patterns {
		for _, p := range c.Permissions {
			if match(pat, p) {
				set[p.Code] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func match(pat string, p Permission) bool {
	if pat == "*" || pat == p.Code {
		return true
	}
	parts := strings.Split(pat, ".")
	switch len(parts) {
	case 2:
		return parts[1] == "*" && parts[0] == p.Module
	case 3:
		return (parts[0] == "*" || parts[0] == p.Module) &&
			(parts[1] == "*" || parts[1] == p.Object) &&
			(parts[2] == "*" || parts[2] == p.Action)
	}
	return false
}

func domainOf(roleCode string) string {
	switch {
	case strings.HasPrefix(roleCode, "engineering"), roleCode == "technician":
		return "engineering"
	case strings.HasPrefix(roleCode, "security"):
		return "security"
	case strings.HasPrefix(roleCode, "housekeeping"):
		return "housekeeping"
	case roleCode == "organization_admin":
		return "admin"
	default:
		return "management"
	}
}
