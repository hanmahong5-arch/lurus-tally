package digest

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	apptodo "github.com/hanmahong5-arch/lurus-tally/internal/app/todo"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// TodoUseCase is the surface the todo summary endpoint calls.
type TodoUseCase interface {
	Execute(ctx context.Context, tenantID uuid.UUID) (apptodo.Summary, error)
}

// WithTodo mounts GET /todo/summary. Without it the route is simply absent.
func (h *Handler) WithTodo(uc TodoUseCase) *Handler {
	h.todo = uc
	return h
}

// WithAIEnabled overrides how the todo summary decides whether the AI assistant
// is available (default: the NEWAPI_API_KEY switch the AI wiring itself uses).
func (h *Handler) WithAIEnabled(f func() bool) *Handler {
	h.aiEnabled = f
	return h
}

// WithCloudPrintAvailable reports whether the platform has a cloud receipt printer account.
func (h *Handler) WithCloudPrintAvailable(f func() bool) *Handler {
	h.cloudPrint = f
	return h
}

func (h *Handler) aiOn() bool {
	if h.aiEnabled != nil {
		return h.aiEnabled()
	}
	return os.Getenv("NEWAPI_API_KEY") != ""
}

// cst is the shop calendar (Asia/Shanghai); a fixed zone avoids tzdata on scratch images.
var cst = time.FixedZone("CST", 8*3600)

type todoOnboardingResp struct {
	HasProduct  bool `json:"has_product"`
	HasPurchase bool `json:"has_purchase"`
	HasSale     bool `json:"has_sale"`
	HasPayment  bool `json:"has_payment"`
	HasDemo     bool `json:"has_demo"`
}

type todoBillResp struct {
	ID          uuid.UUID `json:"id"`
	BillNo      string    `json:"bill_no"`
	PartnerName string    `json:"partner_name"`
	TotalAmount string    `json:"total_amount"`
	BillDate    string    `json:"bill_date"`
}

type todoBillSection struct {
	Count int            `json:"count"`
	Items []todoBillResp `json:"items"`
}

type todoStockResp struct {
	ProductID    uuid.UUID `json:"product_id"`
	ProductName  string    `json:"product_name"`
	AvailableQty string    `json:"available_qty"`
	ReorderPoint string    `json:"reorder_point"`
	DaysOfSupply string    `json:"days_of_supply"`
}

type todoStockSection struct {
	Count int             `json:"count"`
	Items []todoStockResp `json:"items"`
}

type todoDebtorResp struct {
	PartnerID      *uuid.UUID `json:"partner_id"`
	PartnerName    string     `json:"partner_name"`
	OverdueAmount  string     `json:"overdue_amount"`
	UnpaidAmount   string     `json:"unpaid_amount"`
	OldestBillDate string     `json:"oldest_bill_date"`
}

type todoDebtorSection struct {
	Count int              `json:"count"`
	Items []todoDebtorResp `json:"items"`
}

type todoNegativeStockResp struct {
	ProductID     uuid.UUID `json:"product_id"`
	ProductName   string    `json:"product_name"`
	WarehouseName string    `json:"warehouse_name"`
	OnHand        string    `json:"on_hand"`
}

type todoNegativeStockSection struct {
	Count int                     `json:"count"`
	Items []todoNegativeStockResp `json:"items"`
}

type todoKPIResp struct {
	TodaySales       string `json:"today_sales"`
	TodayCollected   string `json:"today_collected"`
	ReceivableUnpaid string `json:"receivable_unpaid"`
	MonthUninvoiced  string `json:"month_uninvoiced"`
}

type todoResp struct {
	DraftPurchases   todoBillSection          `json:"draft_purchases"`
	DraftSales       todoBillSection          `json:"draft_sales"`
	LowStock         todoStockSection         `json:"low_stock"`
	OverdueCustomers todoDebtorSection        `json:"overdue_customers"`
	UninvoicedSales  todoBillSection          `json:"uninvoiced_sales"`
	NegativeStock    todoNegativeStockSection `json:"negative_stock"`
	KPIs             todoKPIResp              `json:"kpis"`
	Total            int                      `json:"total"`
	HasData          bool                     `json:"has_data"`
	Onboarding       todoOnboardingResp       `json:"onboarding"`
	AIEnabled        bool                     `json:"ai_enabled"`
	CloudPrintAvail  bool                     `json:"cloud_print_available"`
	Failed           []string                 `json:"failed"`
	GeneratedAt      time.Time                `json:"generated_at"`
}

// GetTodoSummary handles GET /api/v1/todo/summary.
func (h *Handler) GetTodoSummary(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "detail": "tenant_id required"})
		return
	}
	s, err := h.todo.Execute(c.Request.Context(), tenantID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	failed := s.Failed
	if failed == nil {
		failed = []string{}
	}
	c.JSON(http.StatusOK, todoResp{
		DraftPurchases:   toBillSection(s.DraftPurchases),
		DraftSales:       toBillSection(s.DraftSales),
		LowStock:         toStockSection(s.LowStock),
		OverdueCustomers: toDebtorSection(s.OverdueCustomers),
		UninvoicedSales:  toBillSection(s.UninvoicedSales),
		NegativeStock:    toNegativeStockSection(s.NegativeStock),
		KPIs: todoKPIResp{
			TodaySales:       s.KPIs.TodaySales.StringFixed(2),
			TodayCollected:   s.KPIs.TodayCollected.StringFixed(2),
			ReceivableUnpaid: s.KPIs.ReceivableUnpaid.StringFixed(2),
			MonthUninvoiced:  s.KPIs.MonthUninvoiced.StringFixed(2),
		},
		Total:   s.Total,
		HasData: s.HasData,
		Onboarding: todoOnboardingResp{
			HasProduct: s.Onboarding.HasProduct, HasPurchase: s.Onboarding.HasPurchase,
			HasSale: s.Onboarding.HasSale, HasPayment: s.Onboarding.HasPayment, HasDemo: s.Onboarding.HasDemo,
		},
		AIEnabled:       h.aiOn(),
		CloudPrintAvail: h.cloudPrint != nil && h.cloudPrint(),
		Failed:          failed,
		GeneratedAt:     s.GeneratedAt,
	})
}

func toBillSection(in apptodo.BillSection) todoBillSection {
	out := todoBillSection{Count: in.Count, Items: make([]todoBillResp, 0, len(in.Items))}
	for _, b := range in.Items {
		out.Items = append(out.Items, todoBillResp{
			ID: b.ID, BillNo: b.BillNo, PartnerName: b.PartnerName,
			TotalAmount: b.TotalAmount.StringFixed(2), BillDate: b.BillDate.In(cst).Format("2006-01-02"),
		})
	}
	return out
}

func toStockSection(in apptodo.StockSection) todoStockSection {
	out := todoStockSection{Count: in.Count, Items: make([]todoStockResp, 0, len(in.Items))}
	for _, i := range in.Items {
		out.Items = append(out.Items, todoStockResp{
			ProductID: i.ProductID, ProductName: i.ProductName, AvailableQty: i.AvailableQty,
			ReorderPoint: i.ReorderPoint, DaysOfSupply: i.DaysOfSupply,
		})
	}
	return out
}

func toNegativeStockSection(in apptodo.NegativeStockSection) todoNegativeStockSection {
	out := todoNegativeStockSection{Count: in.Count, Items: make([]todoNegativeStockResp, 0, len(in.Items))}
	for _, i := range in.Items {
		out.Items = append(out.Items, todoNegativeStockResp{
			ProductID: i.ProductID, ProductName: i.ProductName, WarehouseName: i.WarehouseName, OnHand: i.OnHand,
		})
	}
	return out
}

func toDebtorSection(in apptodo.DebtorSection) todoDebtorSection {
	out := todoDebtorSection{Count: in.Count, Items: make([]todoDebtorResp, 0, len(in.Items))}
	for _, d := range in.Items {
		oldest := ""
		if d.OldestBillDate != nil {
			oldest = d.OldestBillDate.In(cst).Format("2006-01-02")
		}
		out.Items = append(out.Items, todoDebtorResp{
			PartnerID: d.PartnerID, PartnerName: d.PartnerName,
			OverdueAmount: d.Overdue.StringFixed(2), UnpaidAmount: d.Unpaid.StringFixed(2), OldestBillDate: oldest,
		})
	}
	return out
}
