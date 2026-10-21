package team

import (
	"context"
	"errors"
	"testing"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

// The platform answering "not a member" about the shop's own owner is a data
// problem on the platform side (an unresolved account, a missing row), not a
// removal: the owner keeps the shop and the answer is logged.
func TestResolveRole_OwnerNotMemberOnPlatform_KeepsShop(t *testing.T) {
	e := newEnv(t)
	delete(e.org.members, 1)
	role, err := e.svc.ResolveRole(context.Background(), e.tenant, "owner")
	if err != nil || role != domain.RoleOwner {
		t.Fatalf("owner must keep the shop: got %v %v", role, err)
	}
	if _, ok := e.store.maps["owner"]; !ok {
		t.Fatal("the owner's local mapping must stay")
	}
}

// Only the local owner row is trusted that way: a manager the platform no
// longer lists is still out.
func TestResolveRole_NonOwnerNotMemberOnPlatform_IsOut(t *testing.T) {
	e := newEnv(t)
	delete(e.org.members, 2)
	if _, err := e.svc.ResolveRole(context.Background(), e.tenant, "mgr"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("want ErrNotMember, got %v", err)
	}
}
