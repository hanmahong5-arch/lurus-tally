package audit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appaudit "github.com/hanmahong5-arch/lurus-tally/internal/app/audit"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

type memRepo struct {
	rows     []appaudit.Row
	appended []appaudit.Entry
	lastF    appaudit.Filter
}

func (m *memRepo) Append(_ context.Context, e appaudit.Entry) error {
	m.appended = append(m.appended, e)
	return nil
}

func (m *memRepo) List(_ context.Context, _ uuid.UUID, f appaudit.Filter) ([]appaudit.Row, int, error) {
	m.lastF = f
	return m.rows, len(m.rows), nil
}

type names map[uuid.UUID]string

func (n names) MemberNames(context.Context, uuid.UUID) (map[uuid.UUID]string, error) { return n, nil }

var tenant = uuid.New()

func engine(role team.Role, repo *memRepo, sub string, nm names) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	api := e.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, tenant)
		c.Set(middleware.CtxKeyIDPSubject, sub)
		c.Set(middleware.CtxKeyShopRole, role)
		c.Next()
	}, middleware.EnforceRoutePerms(), Recorder(repo))
	New(appaudit.NewListUseCase(repo, nm)).RegisterRoutes(api)
	api.POST("/purchase-bills/:id/approve", func(c *gin.Context) { c.JSON(200, gin.H{"status": "approved"}) })
	api.POST("/purchase-bills/:id/cancel", func(c *gin.Context) { c.JSON(409, gin.H{"error": "x"}) })
	api.POST("/purchase-bills", func(c *gin.Context) {
		c.JSON(201, gin.H{"bill_id": "11111111-1111-1111-1111-111111111111", "bill_no": "PO-9"})
	})
	return e
}

func do(e *gin.Engine, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestList_ClerkForbiddenInChinese(t *testing.T) {
	w := do(engine(team.RoleClerk, &memRepo{}, "1", nil), "GET", "/api/v1/audit")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "操作记录") {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestList_OwnerSeesChineseOnly(t *testing.T) {
	boss := uuid.New()
	repo := &memRepo{rows: []appaudit.Row{
		{ID: uuid.New(), UserID: boss, Action: "bill.approved", Resource: "purchase_bill", ResourceID: uuid.New(), ResourceNo: "PO-1"},
		{ID: uuid.New(), UserID: boss, Action: "bill.voided", Resource: "sale_bill", ResourceNo: "SL-1"},
	}}
	w := do(engine(team.RoleOwner, repo, "1", names{boss: "老王"}), "GET", "/api/v1/audit?limit=10&resource=purchase_bill&from=2026-10-01&to=2026-10-02")
	if w.Code != 200 {
		t.Fatalf("code=%d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"actor_name":"老王"`, `"action_label":"审核"`, `"action_label":"作废"`, `"resource_label":"采购单"`, `"resource_no":"PO-1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
	latin := regexp.MustCompile(`[A-Za-z]`)
	for _, key := range []string{"action_label", "resource_label", "actor_name", "summary"} {
		re := regexp.MustCompile(`"` + key + `":"([^"]*)"`)
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			if latin.MatchString(m[1]) {
				t.Errorf("%s shows latin text %q", key, m[1])
			}
		}
	}
	if repo.lastF.Resource != "purchase_bill" || repo.lastF.Limit != 10 || repo.lastF.From == nil || repo.lastF.To == nil || !repo.lastF.To.After(*repo.lastF.From) {
		t.Errorf("filter %+v", repo.lastF)
	}
}

func TestList_BadParams(t *testing.T) {
	e := engine(team.RoleOwner, &memRepo{}, "1", nil)
	for _, q := range []string{"limit=0", "limit=x", "offset=-1", "resource=nope", "resource_id=abc", "from=yesterday", "to=1/2"} {
		if w := do(e, "GET", "/api/v1/audit?"+q); w.Code != 400 {
			t.Errorf("%s -> %d", q, w.Code)
		}
	}
}

func TestRecorder_ApproveAndCreate(t *testing.T) {
	repo := &memRepo{}
	e := engine(team.RoleOwner, repo, "1001", nil)
	id := "22222222-2222-2222-2222-222222222222"
	do(e, "POST", "/api/v1/purchase-bills/"+id+"/approve")
	do(e, "POST", "/api/v1/purchase-bills/"+id+"/cancel") // 409: not recorded
	do(e, "POST", "/api/v1/purchase-bills")
	if len(repo.appended) != 2 {
		t.Fatalf("appended %d: %+v", len(repo.appended), repo.appended)
	}
	a := repo.appended[0]
	if a.Action != "bill.approved" || a.Resource != "purchase_bill" || a.ResourceID.String() != id || a.TenantID != tenant || a.UserID == uuid.Nil {
		t.Errorf("approve entry %+v", a)
	}
	if c := repo.appended[1]; c.Action != "bill.created" || c.ResourceID.String() != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("create entry %+v", c)
	}
}
