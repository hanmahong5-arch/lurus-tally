package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	appteam "github.com/hanmahong5-arch/lurus-tally/internal/app/team"
)

func newOrgTestClient(t *testing.T, h http.HandlerFunc) *OrgClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	return NewOrgClient(c)
}

func TestOrgClient_EnsureOrg(t *testing.T) {
	var got map[string]any
	oc := newOrgTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/internal/v1/organizations" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("bad request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"org_id": 42}`))
	})
	id, err := oc.EnsureOrg(context.Background(), "小店", 7, "tally", "tid")
	if err != nil || id != 42 {
		t.Fatalf("%d %v", id, err)
	}
	if got["owner_account_id"].(float64) != 7 || got["product"] != "tally" || got["external_ref"] != "tid" {
		t.Fatalf("body %v", got)
	}
}

func TestOrgClient_MembersBeyond4KB(t *testing.T) {
	oc := newOrgTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var sb strings.Builder
		sb.WriteString(`{"items":[`)
		for i := 0; i < 120; i++ {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(`{"account_id":1,"role":"member","product_roles":{"tally":"clerk"},"display_name":"某某某某某某某某","email_masked":"a***@x.cn","phone_masked":"","joined_at":"2026-10-10T00:00:00Z"}`)
		}
		sb.WriteString(`]}`)
		if sb.Len() < 8000 {
			t.Errorf("fixture too small: %d", sb.Len())
		}
		_, _ = w.Write([]byte(sb.String()))
	})
	ms, err := oc.ListMembers(context.Background(), 5)
	if err != nil || len(ms) != 120 || ms[0].ProductRoles["tally"] != "clerk" {
		t.Fatalf("%d %v", len(ms), err)
	}
}

func TestOrgClient_ErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
		call   func(oc *OrgClient) error
	}{
		{404, `{"error":"not_member"}`, appteam.ErrNotMember, func(oc *OrgClient) error { _, e := oc.GetMember(context.Background(), 1, 2); return e }},
		{403, `{"error":"forbidden"}`, appteam.ErrForbidden, func(oc *OrgClient) error {
			return oc.UpdateMember(context.Background(), 1, 2, 3, "admin", nil)
		}},
		{409, `{"error":"last_owner"}`, appteam.ErrLastOwner, func(oc *OrgClient) error { return oc.RemoveMember(context.Background(), 1, 2, 2) }},
		{410, `{"error":"invite_invalid"}`, appteam.ErrInviteInvalid, func(oc *OrgClient) error {
			_, e := oc.AcceptInvite(context.Background(), "t", 9)
			return e
		}},
		{404, `{"error":"not_found"}`, appteam.ErrInviteInvalid, func(oc *OrgClient) error {
			return oc.RevokeInvite(context.Background(), 1, uuid.New(), 2)
		}},
		{401, `{"error":"unauthorized"}`, appteam.ErrUnavailable, func(oc *OrgClient) error { _, e := oc.ListMembers(context.Background(), 1); return e }},
		// A route the platform does not serve (not deployed yet) has no business code:
		// that is "unavailable", never "not a member" / "invite invalid".
		{404, ``, appteam.ErrUnavailable, func(oc *OrgClient) error { _, e := oc.GetMember(context.Background(), 1, 2); return e }},
		{404, `404 page not found`, appteam.ErrUnavailable, func(oc *OrgClient) error { _, e := oc.ListMembers(context.Background(), 1); return e }},
		{405, ``, appteam.ErrUnavailable, func(oc *OrgClient) error { return oc.RemoveMember(context.Background(), 1, 2, 2) }},
		{501, ``, appteam.ErrUnavailable, func(oc *OrgClient) error { _, e := oc.ListInvites(context.Background(), 1); return e }},
		{404, ``, appteam.ErrUnavailable, func(oc *OrgClient) error {
			return oc.RevokeInvite(context.Background(), 1, uuid.New(), 2)
		}},
		{404, ``, appteam.ErrUnavailable, func(oc *OrgClient) error {
			_, e := oc.AcceptInvite(context.Background(), "t", 9)
			return e
		}},
	}
	for i, c := range cases {
		oc := newOrgTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		})
		if err := c.call(oc); !errors.Is(err, c.want) {
			t.Errorf("case %d: want %v got %v", i, c.want, err)
		}
	}
}

func TestOrgClient_Unconfigured(t *testing.T) {
	var nilClient *Client
	oc := NewOrgClient(nilClient)
	if _, err := oc.ListMembers(context.Background(), 1); !errors.Is(err, appteam.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestOrgClient_InviteRoundtripShapes(t *testing.T) {
	iid := uuid.New()
	oc := newOrgTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/invites"):
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["actor_account_id"].(float64) != 3 || b["ttl_hours"].(float64) != 72 {
				t.Errorf("body %v", b)
			}
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"invite_id":"` + iid.String() + `","token":"tk","expires_at":"2026-10-13T00:00:00Z"}`))
		case r.Method == "GET":
			_, _ = w.Write([]byte(`{"items":[{"invite_id":"` + iid.String() + `","role":"member","product_roles":{"tally":"cashier"},"invited_by_account_id":3,"expires_at":"2026-10-13T00:00:00Z","created_at":"2026-10-10T00:00:00Z"}]}`))
		case r.Method == "DELETE":
			if r.URL.Query().Get("actor_account_id") != "3" || !strings.HasSuffix(r.URL.Path, "/invites/"+iid.String()) {
				t.Errorf("bad delete %s", r.URL.String())
			}
			w.WriteHeader(204)
		}
	})
	ctx := context.Background()
	res, err := oc.CreateInvite(ctx, 5, 3, "member", map[string]string{"tally": "cashier"}, 72)
	if err != nil || res.Token != "tk" || res.InviteID != iid {
		t.Fatalf("%+v %v", res, err)
	}
	ivs, err := oc.ListInvites(ctx, 5)
	if err != nil || len(ivs) != 1 || ivs[0].ProductRoles["tally"] != "cashier" {
		t.Fatalf("%+v %v", ivs, err)
	}
	if err := oc.RevokeInvite(ctx, 5, iid, 3); err != nil {
		t.Fatal(err)
	}
}

func TestParseOrgMemberEvent(t *testing.T) {
	ev, ok := ParseOrgMemberEvent(appteam.EventRemoved, []byte(`{"event_type":"identity.org.member_removed","payload":{"org_id":9,"account_id":4,"role":"member"}}`))
	if !ok || ev.OrgID != 9 || ev.AccountID != 4 || ev.Type != appteam.EventRemoved {
		t.Fatalf("%+v %v", ev, ok)
	}
	if ev, ok := ParseOrgMemberEvent(appteam.EventRoleChanged, []byte(`{"org_id":9,"account_id":4}`)); !ok || ev.OrgID != 9 {
		t.Fatalf("bare payload: %+v %v", ev, ok)
	}
	if _, ok := ParseOrgMemberEvent("identity.org.created", []byte(`{"org_id":9}`)); ok {
		t.Fatal("other subjects must be ignored")
	}
	if _, ok := ParseOrgMemberEvent(appteam.EventJoined, []byte(`not json`)); ok {
		t.Fatal("bad body must be ignored")
	}
}

func TestDevOrgClient_RequiresDevMode(t *testing.T) {
	if _, err := NewDevOrgClient(nil, false); err == nil {
		t.Fatal("must refuse outside dev mode")
	}
}

func TestMaskEmail(t *testing.T) {
	if got := maskEmail("alice@example.com"); got != "a***@example.com" {
		t.Fatalf("got %q", got)
	}
	if maskEmail("nope") != "" {
		t.Fatal("no @ => empty")
	}
}

func TestOrgClient_DissolveOrg(t *testing.T) {
	var got map[string]any
	oc := newOrgTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/internal/v1/organizations/42/dissolve" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("bad request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(204)
	})
	if err := oc.DissolveOrg(context.Background(), 42, 7); err != nil {
		t.Fatal(err)
	}
	if got["actor_account_id"].(float64) != 7 || len(got) != 1 {
		t.Fatalf("body %v", got)
	}
}

func TestOrgClient_DissolveOrg_ErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
	}{
		{409, `{"error":"org_not_dissolvable"}`, appteam.ErrOrgNotDissolvable},
		{409, `{"error":"org_dissolved"}`, appteam.ErrInviteInvalid},
		{403, `{"error":"forbidden"}`, appteam.ErrForbidden},
		{401, `{"error":"unauthorized"}`, appteam.ErrUnavailable},
		// a platform that does not serve the route yet: unavailable, not a business answer
		{404, ``, appteam.ErrUnavailable},
		{405, ``, appteam.ErrUnavailable},
	}
	for i, c := range cases {
		oc := newOrgTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		})
		if err := oc.DissolveOrg(context.Background(), 1, 2); !errors.Is(err, c.want) {
			t.Errorf("case %d: want %v got %v", i, c.want, err)
		}
	}
}

func TestOrgClient_DissolveOrg_Unconfigured(t *testing.T) {
	var nilClient *Client
	if err := NewOrgClient(nilClient).DissolveOrg(context.Background(), 1, 2); !errors.Is(err, appteam.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

// A dissolved org answers 409 org_dissolved on invite create/accept: its invites are dead.
func TestOrgClient_InviteErrorMapping_OrgDissolved(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
	}{
		{409, `{"error":"org_dissolved"}`, appteam.ErrInviteInvalid},
		{403, `{"error":"forbidden"}`, appteam.ErrForbidden},
		{404, ``, appteam.ErrUnavailable},
	}
	for i, c := range cases {
		oc := newOrgTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		})
		if _, err := oc.CreateInvite(context.Background(), 1, 2, "member", nil, 72); !errors.Is(err, c.want) {
			t.Errorf("CreateInvite case %d: want %v got %v", i, c.want, err)
		}
		if _, err := oc.AcceptInvite(context.Background(), "tk", 2); !errors.Is(err, c.want) {
			t.Errorf("AcceptInvite case %d: want %v got %v", i, c.want, err)
		}
	}
}
