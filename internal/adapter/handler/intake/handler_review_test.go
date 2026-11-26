package intake

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

type reviewAPI struct {
	doc       *domain.Document
	acceptErr error
	partner   *uuid.UUID
	direction string
	choose    bool
	gets      int
}

func (reviewAPI) Capture(context.Context, domain.CaptureInput) (*domain.Document, error) {
	return nil, nil
}
func (a reviewAPI) List(context.Context, uuid.UUID, *uuid.UUID, int, int) ([]*domain.Document, int, error) {
	if a.doc == nil {
		return nil, 0, nil
	}
	return []*domain.Document{a.doc}, 1, nil
}
func (reviewAPI) CountOpen(context.Context, uuid.UUID, *uuid.UUID) (int, error) { return 0, nil }
func (a *reviewAPI) Get(_ context.Context, _, _ uuid.UUID, _, partner *uuid.UUID, direction string, choose bool) (*domain.Document, error) {
	a.gets++
	a.partner, a.direction, a.choose = partner, direction, choose
	return a.doc, nil
}
func (reviewAPI) OpenFile(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID) (io.ReadCloser, string, error) {
	return nil, "", domain.ErrNotFound
}
func (reviewAPI) Lock(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error { return nil }
func (a reviewAPI) Accept(context.Context, domain.AcceptInput) (*domain.AcceptResult, error) {
	return nil, a.acceptErr
}
func (reviewAPI) Reject(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) (*domain.Document, error) {
	return nil, nil
}

func reviewEngine(role team.Role, api API) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	g := e.Group("/api/v1")
	g.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, uuid.New())
		c.Set(middleware.CtxKeyShopRole, role)
		c.Next()
	}, middleware.EnforceRoutePerms())
	New(api).RegisterRoutes(g)
	return e
}

func moneyDoc() *domain.Document {
	return &domain.Document{
		ID: uuid.New(), Source: domain.SourceFile, Direction: domain.DirectionIn, Status: domain.StatusNeedsReview,
		InvoiceNo: "NO-1", ContentType: "application/pdf",
		Parsed: domain.Parsed{
			TotalAmount: "113.00", AmountExclTax: "100.00", TaxAmount: "13.00", MoneyOK: true,
			Lines:   []domain.ParsedLine{{Name: "石楠", UnitPrice: "50.00"}},
			Matches: []domain.Match{{BillID: uuid.NewString(), BillNo: "S-1", Amount: "113.00"}},
		},
		OpenBills: []domain.OpenBill{{ID: uuid.New(), BillNo: "PO-1", Status: 2, Total: decimal.RequireFromString("113")}},
	}
}

func TestList_ClerkDoesNotSeeCost(t *testing.T) {
	e := reviewEngine(team.RoleClerk, &reviewAPI{doc: moneyDoc()})
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/intake", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "NO-1") || !strings.Contains(body, "石楠") {
		t.Fatalf("%d %s", w.Code, body)
	}
	for _, hidden := range []string{"113.00", "100.00", "13.00", "50.00"} {
		if strings.Contains(body, hidden) {
			t.Fatalf("clerk list leaked %s in %s", hidden, body)
		}
	}
}

func TestGet_PartnerQueryAndBadID(t *testing.T) {
	api := &reviewAPI{doc: moneyDoc()}
	e := reviewEngine(team.RoleOwner, api)
	partner := uuid.New()
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/intake/"+api.doc.ID.String()+"?partner_id="+partner.String()+"&direction=out", nil))
	if w.Code != http.StatusOK || api.gets != 1 || api.partner == nil || *api.partner != partner || api.direction != "out" || !api.choose {
		t.Fatalf("code=%d gets=%d partner=%v dir=%s choose=%v body=%s", w.Code, api.gets, api.partner, api.direction, api.choose, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PO-1") || !strings.Contains(w.Body.String(), "113.00") || !strings.Contains(w.Body.String(), "application/pdf") {
		t.Fatalf("owner detail %s", w.Body.String())
	}
	bad := httptest.NewRecorder()
	e.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/api/v1/intake/"+api.doc.ID.String()+"?partner_id=nope", nil))
	if bad.Code != http.StatusBadRequest || api.gets != 1 {
		t.Fatalf("bad partner code=%d gets=%d", bad.Code, api.gets)
	}
}

func TestAccept_MoneyAndDuplicateBodies(t *testing.T) {
	e := reviewEngine(team.RoleOwner, &reviewAPI{acceptErr: fmt.Errorf("%w: 不含税金额、税额、价税合计对不齐", domain.ErrMoney)})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/intake/"+uuid.NewString()+"/accept", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "money_mismatch") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	existing := uuid.New()
	e = reviewEngine(team.RoleFinance, &reviewAPI{acceptErr: &domain.Duplicate{
		ExistingID: existing, Kind: "invoice", Code: domain.CodeDuplicate, InvoiceNo: "24317000000012345678", Message: "这张票已经登记过",
	}})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/intake/"+uuid.NewString()+"/accept", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(w, req)
	body := w.Body.String()
	if w.Code != http.StatusConflict || !strings.Contains(body, existing.String()) || !strings.Contains(body, `"existing_kind":"invoice"`) || !strings.Contains(body, `"invoice_no":"24317000000012345678"`) {
		t.Fatalf("%d %s", w.Code, body)
	}
}
