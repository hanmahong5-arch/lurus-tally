package team

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appteam "github.com/hanmahong5-arch/lurus-tally/internal/app/team"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

type fakeSvc struct {
	err       error
	noCashier bool
	lastRole  domain.Role
	lastCall  appteam.Caller
	joinIn    appteam.JoinInput
}

func (f *fakeSvc) ListMembers(_ context.Context, c appteam.Caller) ([]appteam.MemberView, error) {
	f.lastCall = c
	return []appteam.MemberView{{AccountID: 7, Role: domain.RoleCashier, RoleLabel: "收银"}}, f.err
}

func (f *fakeSvc) CashierAllowed(_ context.Context, _ uuid.UUID) bool { return !f.noCashier }

func (f *fakeSvc) UpdateMemberRole(_ context.Context, _ appteam.Caller, _ int64, r domain.Role) error {
	f.lastRole = r
	return f.err
}

func (f *fakeSvc) RemoveMember(_ context.Context, _ appteam.Caller, _ int64) error { return f.err }

func (f *fakeSvc) CreateInvite(_ context.Context, _ appteam.Caller, r domain.Role, _ int) (*appteam.InviteCreated, error) {
	f.lastRole = r
	if f.err != nil {
		return nil, f.err
	}
	return &appteam.InviteCreated{InviteID: uuid.New(), Token: "abc", ExpiresAt: time.Now().Add(time.Hour), ShopName: "阿明五金"}, nil
}

func (f *fakeSvc) ListInvites(_ context.Context, _ appteam.Caller) ([]appteam.InviteView, error) {
	return []appteam.InviteView{}, f.err
}

func (f *fakeSvc) RevokeInvite(_ context.Context, _ appteam.Caller, _ uuid.UUID) error { return f.err }

func (f *fakeSvc) Join(_ context.Context, in appteam.JoinInput) (*appteam.JoinResult, error) {
	f.joinIn = in
	if f.err != nil {
		return nil, f.err
	}
	return &appteam.JoinResult{TenantID: uuid.New(), ShopName: "阿明五金", Role: domain.RoleCashier}, nil
}

func newRouter(h *Handler, sub string, tenant uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1", func(c *gin.Context) {
		if sub != "" {
			c.Set(middleware.CtxKeyIDPSubject, sub)
		}
		if tenant != uuid.Nil {
			c.Set(middleware.CtxKeyTenantID, tenant)
		}
	})
	h.RegisterRoutes(g)
	return r
}

func do(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestListMembers_OK(t *testing.T) {
	f := &fakeSvc{}
	tid := uuid.New()
	w := do(newRouter(New(f), "u1", tid), "GET", "/api/v1/team/members", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"account_id":7`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if f.lastCall.Sub != "u1" || f.lastCall.TenantID != tid {
		t.Fatalf("caller %+v", f.lastCall)
	}
}

func TestAuthRequired(t *testing.T) {
	w := do(newRouter(New(&fakeSvc{}), "", uuid.New()), "GET", "/api/v1/team/members", "")
	if w.Code != 401 {
		t.Fatalf("got %d", w.Code)
	}
	w = do(newRouter(New(&fakeSvc{}), "u1", uuid.Nil), "GET", "/api/v1/team/members", "")
	if w.Code != 403 {
		t.Fatalf("no shop: got %d", w.Code)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		code int
		key  string
	}{
		{appteam.ErrUnavailable, 503, "team_unavailable"},
		{appteam.ErrForbidden, 403, "forbidden"},
		{appteam.ErrNotMember, 404, "not_member"},
		{appteam.ErrAlreadyInShop, 409, "already_in_shop"},
		{appteam.ErrInviteInvalid, 410, "invite_invalid"},
		{appteam.ErrLastOwner, 409, "last_owner"},
		{appteam.ErrInvalidRole, 400, "invalid_role"},
		{appteam.ErrRoleNotForShop, 422, "role_not_for_shop"},
		{appteam.ErrOrgNotReady, 409, "team_not_ready"},
		{errors.New("boom secret detail"), 500, "internal_error"},
	}
	for _, c := range cases {
		w := do(newRouter(New(&fakeSvc{err: c.err}), "u1", uuid.New()), "GET", "/api/v1/team/members", "")
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != c.code || body["error"] != c.key {
			t.Errorf("%v: got %d %v", c.err, w.Code, body)
		}
		msg, _ := body["message"].(string)
		if msg == "" || msg[0] < 0x80 {
			t.Errorf("%v: message must be Chinese, got %q", c.err, msg)
		}
		if c.key == "invalid_role" && msg != "请选择列表里的角色" {
			t.Errorf("invalid_role message = %q", msg)
		}
		if strings.Contains(w.Body.String(), "boom") {
			t.Errorf("internal detail leaked: %s", w.Body.String())
		}
	}
}

func TestUpdateAndRemove(t *testing.T) {
	f := &fakeSvc{}
	r := newRouter(New(f), "u1", uuid.New())
	if w := do(r, "PATCH", "/api/v1/team/members/9", `{"role":"finance"}`); w.Code != 200 || f.lastRole != domain.RoleFinance {
		t.Fatalf("%d %s %s", w.Code, w.Body.String(), f.lastRole)
	}
	if w := do(r, "PATCH", "/api/v1/team/members/9", `{}`); w.Code != 400 {
		t.Fatalf("empty role: %d", w.Code)
	}
	if w := do(r, "PATCH", "/api/v1/team/members/x", `{"role":"clerk"}`); w.Code != 400 {
		t.Fatalf("bad id: %d", w.Code)
	}
	if w := do(r, "DELETE", "/api/v1/team/members/9", ""); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
}

func TestCreateInvite_ReturnsLink(t *testing.T) {
	f := &fakeSvc{}
	r := newRouter(New(f), "u1", uuid.New())
	w := do(r, "POST", "/api/v1/team/invites", `{"role":"cashier"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	// shop and role ride along only to be displayed on the join page.
	wantLink := "/join?token=abc&i=" + body["invite_id"].(string) + "&shop=" + url.QueryEscape("阿明五金") + "&role=" + url.QueryEscape("收银")
	if body["link"] != wantLink || body["role_label"] != "收银" {
		t.Fatalf("body %v", body)
	}
	if _, has := body["token"]; has {
		t.Fatal("raw token must only appear inside the link")
	}
	if w := do(r, "POST", "/api/v1/team/invites", `{"role":"clerk","ttl_hours":500}`); w.Code != 400 {
		t.Fatalf("ttl: %d", w.Code)
	}
	if w := do(r, "DELETE", "/api/v1/team/invites/not-a-uuid", ""); w.Code != 400 {
		t.Fatalf("bad invite id: %d", w.Code)
	}
	if w := do(r, "DELETE", "/api/v1/team/invites/"+uuid.NewString(), ""); w.Code != 204 {
		t.Fatalf("revoke: %d", w.Code)
	}
	if w := do(r, "GET", "/api/v1/team/invites", ""); w.Code != 200 {
		t.Fatalf("list: %d", w.Code)
	}
}

func TestJoin_NoTenantNeeded(t *testing.T) {
	f := &fakeSvc{}
	r := newRouter(New(f), "newbie", uuid.Nil)
	w := do(r, "POST", "/api/v1/team/join", `{"token":"abc"}`)
	if w.Code != 200 || f.joinIn.Sub != "newbie" || f.joinIn.Token != "abc" {
		t.Fatalf("%d %s %+v", w.Code, w.Body.String(), f.joinIn)
	}
	if w := do(r, "POST", "/api/v1/team/join", `{}`); w.Code != 400 {
		t.Fatalf("missing token: %d", w.Code)
	}
	f.err = appteam.ErrAlreadyInShop
	if w := do(r, "POST", "/api/v1/team/join", `{"token":"abc"}`); w.Code != 409 {
		t.Fatalf("conflict: %d", w.Code)
	}
}

func TestNilServiceIs503(t *testing.T) {
	r := newRouter(New(nil), "u1", uuid.New())
	if w := do(r, "GET", "/api/v1/team/members", ""); w.Code != 503 {
		t.Fatalf("got %d", w.Code)
	}
	if w := do(r, "POST", "/api/v1/team/join", `{"token":"a"}`); w.Code != 503 {
		t.Fatalf("join: %d", w.Code)
	}
}

func TestJoin_ReturnsShopNameAndRole(t *testing.T) {
	r := newRouter(New(&fakeSvc{}), "newbie", uuid.Nil)
	w := do(r, "POST", "/api/v1/team/join", `{"token":"abc"}`)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["shop_name"] != "阿明五金" || body["role"] != "cashier" || body["role_label"] != "收银" {
		t.Fatalf("body %v", body)
	}
}

func TestJoin_AlreadyInShop_TellsWhatToDo(t *testing.T) {
	cases := []struct {
		err  appteam.AlreadyInShopError
		want []string
	}{
		{appteam.AlreadyInShopError{IsOwner: true}, []string{"开过自己的店", "换一个没开过店的账号", "联系客服"}},
		{appteam.AlreadyInShopError{IsOwner: true, CanAbandon: true}, []string{"只有示例数据", "放弃", "不能恢复", "老板店里的数据"}},
		{appteam.AlreadyInShopError{}, []string{"已经是一个店铺的成员", "直接登录使用"}},
	}
	for _, tc := range cases {
		e := tc.err
		w := do(newRouter(New(&fakeSvc{err: &e}), "u1", uuid.Nil), "POST", "/api/v1/team/join", `{"token":"abc"}`)
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != 409 || body["error"] != "already_in_shop" || body["can_abandon_empty_shop"] != e.CanAbandon {
			t.Fatalf("%+v: %d %v", e, w.Code, body)
		}
		text, _ := body["message"].(string)
		action, _ := body["action"].(string)
		for _, s := range tc.want {
			if !strings.Contains(text+action, s) {
				t.Fatalf("%+v: %q missing in %q / %q", e, s, text, action)
			}
		}
		if !e.CanAbandon && strings.Contains(text+action, "确认放弃") {
			t.Fatalf("%+v: must not offer abandoning: %q", e, text)
		}
	}
}

func TestJoin_AbandonFlagAndRefusal(t *testing.T) {
	f := &fakeSvc{}
	r := newRouter(New(f), "u1", uuid.Nil)
	if w := do(r, "POST", "/api/v1/team/join", `{"token":"abc","abandon_empty_shop":true}`); w.Code != 200 || !f.joinIn.AbandonEmptyShop {
		t.Fatalf("%d %+v", w.Code, f.joinIn)
	}
	f.err = appteam.ErrShopNotEmpty
	w := do(r, "POST", "/api/v1/team/join", `{"token":"abc","abandon_empty_shop":true}`)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 409 || body["error"] != "shop_not_empty" {
		t.Fatalf("%d %v", w.Code, body)
	}
}

func TestJoin_AlreadyMemberHereAndInviteIDPassedOn(t *testing.T) {
	id := uuid.New()
	f := &fakeSvc{}
	r := newRouter(New(f), "u1", uuid.Nil)
	if w := do(r, "POST", "/api/v1/team/join", `{"token":"abc","invite_id":"`+id.String()+`"}`); w.Code != 200 || f.joinIn.InviteID != id {
		t.Fatalf("%d %+v", w.Code, f.joinIn)
	}
	if w := do(r, "POST", "/api/v1/team/join", `{"token":"abc","invite_id":"zzz"}`); w.Code != 200 || f.joinIn.InviteID != uuid.Nil {
		t.Fatalf("bad id must be ignored: %d %+v", w.Code, f.joinIn)
	}
	f.err = appteam.ErrAlreadyMemberHere
	w := do(r, "POST", "/api/v1/team/join", `{"token":"abc"}`)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 409 || body["error"] != "already_member_here" || strings.Contains(w.Body.String(), "放弃") {
		t.Fatalf("%d %v", w.Code, body)
	}
}

func TestListMembers_ReportsCashierAllowed(t *testing.T) {
	for _, noCashier := range []bool{false, true} {
		w := do(newRouter(New(&fakeSvc{noCashier: noCashier}), "u1", uuid.New()), "GET", "/api/v1/team/members", "")
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != 200 || body["cashier_allowed"] != !noCashier {
			t.Fatalf("noCashier=%v: %d %s", noCashier, w.Code, w.Body.String())
		}
	}
}

func TestRoleNotForShop_MessageIsChinese(t *testing.T) {
	w := do(newRouter(New(&fakeSvc{err: appteam.ErrRoleNotForShop}), "u1", uuid.New()), "POST", "/api/v1/team/invites", `{"role":"cashier"}`)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "收银只在零售店的收银台使用，请选店员") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
