// Package team holds the shop-level roles and the mapping between them and the
// platform organization membership (role + product_roles).
package team

import (
	"errors"
	"fmt"
)

// Role is a person's role inside one shop.
type Role string

const (
	RoleOwner   Role = "owner"   // 老板: everything, incl. billing and members
	RoleManager Role = "manager" // 店长: everything except billing/transfer
	RoleClerk   Role = "clerk"   // 店员
	RoleCashier Role = "cashier" // 收银
	RoleFinance Role = "finance" // 财务
)

// ProductKey is the product_roles key Tally owns on a platform membership.
const ProductKey = "tally"

// Platform org roles.
const (
	PlatformOwner  = "owner"
	PlatformAdmin  = "admin"
	PlatformMember = "member"
)

// ErrRoleNotAssignable is returned when a role cannot be handed out through the team API.
var ErrRoleNotAssignable = errors.New("team: role not assignable")

// Label is the shop-facing Chinese name.
func (r Role) Label() string {
	switch r {
	case RoleOwner:
		return "老板"
	case RoleManager:
		return "店长"
	case RoleCashier:
		return "收银"
	case RoleFinance:
		return "财务"
	default:
		return "店员"
	}
}

// ManagesTeam reports whether the role may use the team management API.
func (r Role) ManagesTeam() bool { return r == RoleOwner || r == RoleManager }

// Valid reports whether r is one of the five known roles.
func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleManager, RoleClerk, RoleCashier, RoleFinance:
		return true
	}
	return false
}

// NormalizeLegacy folds a user_identity_mapping.role value (which may still be
// a pre-000059 value) into a Role. Unknown values get the least privilege.
func NormalizeLegacy(role string, isOwner bool) Role {
	if isOwner {
		return RoleOwner
	}
	switch role {
	case "owner":
		return RoleOwner
	case "admin", "manager":
		return RoleManager
	case "cashier":
		return RoleCashier
	case "finance":
		return RoleFinance
	default: // clerk, viewer, unknown
		return RoleClerk
	}
}

// FromPlatform maps a platform membership to a Role. Anything unrecognised is a
// plain clerk — never owner.
func FromPlatform(platformRole string, productRoles map[string]string) Role {
	switch platformRole {
	case PlatformOwner:
		return RoleOwner
	case PlatformAdmin:
		return RoleManager
	case PlatformMember:
		switch Role(productRoles[ProductKey]) {
		case RoleCashier:
			return RoleCashier
		case RoleFinance:
			return RoleFinance
		}
	}
	return RoleClerk
}

// ToPlatform maps an assignable Role to the platform role and product_roles.
// Owner is not assignable (ownership transfer is out of scope for phase 1).
func ToPlatform(r Role) (string, map[string]string, error) {
	switch r {
	case RoleManager:
		return PlatformAdmin, map[string]string{}, nil
	case RoleClerk, RoleCashier, RoleFinance:
		return PlatformMember, map[string]string{ProductKey: string(r)}, nil
	}
	return "", nil, fmt.Errorf("%w: %q", ErrRoleNotAssignable, string(r))
}

// CanManage reports whether actor may change or remove a member holding target.
func CanManage(actor, target Role) bool {
	switch actor {
	case RoleOwner:
		return target != RoleOwner
	case RoleManager:
		return target == RoleClerk || target == RoleCashier || target == RoleFinance
	}
	return false
}

// CanAssign reports whether actor may hand out role (invite or change to).
func CanAssign(actor, role Role) bool {
	switch actor {
	case RoleOwner:
		return role != RoleOwner && role.Valid()
	case RoleManager:
		return role == RoleClerk || role == RoleCashier || role == RoleFinance
	}
	return false
}
