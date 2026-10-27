// Package auth implements OIDC sign-in, server-side sessions and CSRF
// protection.
package auth

import (
	"strings"

	"github.com/felixtrav/sharedcal/internal/authz"
)

// RoleMapping maps IdP groups to application roles.
type RoleMapping struct {
	AdminGroups   []string
	WriterGroups  []string
	ViewerGroups  []string
	DefaultRole   authz.Role
	AllowUnmapped bool
}

// Resolve returns the highest role whose groups intersect the user's groups
// (case-insensitive). When nothing matches it returns the default role, or
// ok=false if unmapped users are refused.
func (m RoleMapping) Resolve(groups []string) (role authz.Role, matched, ok bool) {
	have := make(map[string]bool, len(groups))
	for _, g := range groups {
		have[strings.ToLower(strings.TrimSpace(g))] = true
	}
	anyOf := func(list []string) bool {
		for _, g := range list {
			if have[strings.ToLower(strings.TrimSpace(g))] {
				return true
			}
		}
		return false
	}
	switch {
	case anyOf(m.AdminGroups):
		return authz.Admin, true, true
	case anyOf(m.WriterGroups):
		return authz.Writer, true, true
	case anyOf(m.ViewerGroups):
		return authz.Viewer, true, true
	}
	if !m.AllowUnmapped {
		return "", false, false
	}
	def := m.DefaultRole
	if !def.Valid() {
		def = authz.Viewer
	}
	return def, false, true
}

// GroupsFromClaim extracts a list of group names from a raw claim value,
// accepting an array of strings or a single (comma-separated) string.
func GroupsFromClaim(v any) []string {
	var out []string
	switch t := v.(type) {
	case []any:
		for _, g := range t {
			if s, ok := g.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case []string:
		for _, s := range t {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case string:
		for _, s := range strings.Split(t, ",") {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}
