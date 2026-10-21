package team

import "testing"

func TestNormalizeLegacy(t *testing.T) {
	cases := []struct {
		role    string
		isOwner bool
		want    Role
	}{
		{"admin", true, RoleOwner},
		{"admin", false, RoleManager},
		{"owner", false, RoleOwner},
		{"manager", false, RoleManager},
		{"viewer", false, RoleClerk},
		{"clerk", false, RoleClerk},
		{"cashier", false, RoleCashier},
		{"finance", false, RoleFinance},
		{"", false, RoleClerk},
		{"root", false, RoleClerk},
	}
	for _, c := range cases {
		if got := NormalizeLegacy(c.role, c.isOwner); got != c.want {
			t.Errorf("NormalizeLegacy(%q,%v)=%q want %q", c.role, c.isOwner, got, c.want)
		}
	}
}

func TestFromPlatform(t *testing.T) {
	cases := []struct {
		role string
		pr   map[string]string
		want Role
	}{
		{"owner", nil, RoleOwner},
		{"admin", nil, RoleManager},
		{"member", map[string]string{"tally": "cashier"}, RoleCashier},
		{"member", map[string]string{"tally": "finance"}, RoleFinance},
		{"member", map[string]string{"tally": "clerk"}, RoleClerk},
		{"member", nil, RoleClerk},
		{"member", map[string]string{"tally": "owner"}, RoleClerk},
		{"member", map[string]string{"other": "cashier"}, RoleClerk},
		{"weird", nil, RoleClerk},
	}
	for _, c := range cases {
		if got := FromPlatform(c.role, c.pr); got != c.want {
			t.Errorf("FromPlatform(%q,%v)=%q want %q", c.role, c.pr, got, c.want)
		}
	}
}

func TestToPlatform(t *testing.T) {
	role, pr, err := ToPlatform(RoleManager)
	if err != nil || role != "admin" || len(pr) != 0 {
		t.Fatalf("manager: %q %v %v", role, pr, err)
	}
	role, pr, err = ToPlatform(RoleCashier)
	if err != nil || role != "member" || pr["tally"] != "cashier" {
		t.Fatalf("cashier: %q %v %v", role, pr, err)
	}
	if _, _, err := ToPlatform(RoleOwner); err == nil {
		t.Fatal("owner must not be assignable")
	}
	if _, _, err := ToPlatform(Role("boss")); err == nil {
		t.Fatal("unknown role must be rejected")
	}
}

func TestCanManage(t *testing.T) {
	cases := []struct {
		actor, target Role
		want          bool
	}{
		{RoleOwner, RoleManager, true},
		{RoleOwner, RoleClerk, true},
		{RoleOwner, RoleOwner, false},
		{RoleManager, RoleClerk, true},
		{RoleManager, RoleCashier, true},
		{RoleManager, RoleFinance, true},
		{RoleManager, RoleManager, false},
		{RoleManager, RoleOwner, false},
		{RoleClerk, RoleClerk, false},
		{RoleCashier, RoleClerk, false},
	}
	for _, c := range cases {
		if got := CanManage(c.actor, c.target); got != c.want {
			t.Errorf("CanManage(%s,%s)=%v want %v", c.actor, c.target, got, c.want)
		}
	}
}

func TestCanAssign(t *testing.T) {
	if !CanAssign(RoleOwner, RoleManager) || CanAssign(RoleManager, RoleManager) || !CanAssign(RoleManager, RoleCashier) || CanAssign(RoleManager, RoleOwner) || CanAssign(RoleOwner, RoleOwner) || CanAssign(RoleClerk, RoleClerk) {
		t.Fatal("CanAssign matrix wrong")
	}
}

func TestLabelNoASCII(t *testing.T) {
	for _, r := range []Role{RoleOwner, RoleManager, RoleClerk, RoleCashier, RoleFinance} {
		l := r.Label()
		if l == "" || l[0] < 0x80 {
			t.Errorf("label for %s must be Chinese, got %q", r, l)
		}
	}
}

func TestManagesTeam(t *testing.T) {
	if !RoleOwner.ManagesTeam() || !RoleManager.ManagesTeam() || RoleClerk.ManagesTeam() || RoleCashier.ManagesTeam() || RoleFinance.ManagesTeam() {
		t.Fatal("only owner/manager manage the team")
	}
}
