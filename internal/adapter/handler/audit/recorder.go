package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appaudit "github.com/hanmahong5-arch/lurus-tally/internal/app/audit"
)

// Appender is what the recorder writes through (repo/audit.Repo).
type Appender interface {
	Append(ctx context.Context, e appaudit.Entry) error
}

// rule says what a successful write on one route means. fromBody routes create
// the document, so its id is read from the response ({"bill_id": ...}); the
// others act on the :id in the path.
type rule struct {
	action   string
	resource string
	fromBody bool
}

var rules = map[string]rule{
	"POST /api/v1/purchase-bills":             {appaudit.ActionBillCreated, appaudit.ResourcePurchaseBill, true},
	"PUT /api/v1/purchase-bills/:id":          {appaudit.ActionBillUpdated, appaudit.ResourcePurchaseBill, false},
	"POST /api/v1/purchase-bills/:id/approve": {appaudit.ActionBillApproved, appaudit.ResourcePurchaseBill, false},
	"POST /api/v1/purchase-bills/:id/cancel":  {appaudit.ActionBillVoided, appaudit.ResourcePurchaseBill, false},
	"POST /api/v1/purchase-bills/:id/restore": {appaudit.ActionBillRestored, appaudit.ResourcePurchaseBill, false},
	"POST /api/v1/purchase-bills/:id/return":  {appaudit.ActionBillReturned, appaudit.ResourcePurchaseBill, false},
	"POST /api/v1/sale-bills":                 {appaudit.ActionBillCreated, appaudit.ResourceSaleBill, true},
	"POST /api/v1/sale-bills/quick-checkout":  {appaudit.ActionBillCreated, appaudit.ResourceSaleBill, true},
	"PUT /api/v1/sale-bills/:id":              {appaudit.ActionBillUpdated, appaudit.ResourceSaleBill, false},
	"POST /api/v1/sale-bills/:id/approve":     {appaudit.ActionBillApproved, appaudit.ResourceSaleBill, false},
	"POST /api/v1/sale-bills/:id/cancel":      {appaudit.ActionBillVoided, appaudit.ResourceSaleBill, false},
	"POST /api/v1/sale-bills/:id/restore":     {appaudit.ActionBillRestored, appaudit.ResourceSaleBill, false},
	"POST /api/v1/sale-bills/:id/return":      {appaudit.ActionBillReturned, appaudit.ResourceSaleBill, false},
}

const maxCapture = 64 << 10

// capture tees the response body of create routes so the new id can be read.
type capture struct {
	gin.ResponseWriter
	buf bytes.Buffer
}

func (w *capture) Write(b []byte) (int, error) {
	if w.buf.Len() < maxCapture {
		w.buf.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// Recorder appends one operation-log row after every successful write on a
// bill route. It is best effort: a failed append is logged and never changes the
// answer the shop already got. Replayed idempotent responses are not recorded
// again. Mount it after authentication and the tenant pin.
func Recorder(rec Appender) gin.HandlerFunc {
	if rec == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		r, ok := rules[c.Request.Method+" "+c.FullPath()]
		if !ok {
			c.Next()
			return
		}
		var cap *capture
		if r.fromBody {
			cap = &capture{ResponseWriter: c.Writer}
			c.Writer = cap
		}
		c.Next()

		status := c.Writer.Status()
		if status < 200 || status >= 300 || c.Writer.Header().Get(middleware.HeaderIdempotencyReplay) != "" {
			return
		}
		tenantID := middleware.GetTenantID(c)
		if tenantID == uuid.Nil {
			return
		}
		var resourceID uuid.UUID
		if r.fromBody {
			var body struct {
				BillID string `json:"bill_id"`
			}
			if json.Unmarshal(cap.buf.Bytes(), &body) == nil {
				resourceID, _ = uuid.Parse(body.BillID)
			}
		} else {
			resourceID, _ = uuid.Parse(c.Param("id"))
		}
		if resourceID == uuid.Nil {
			return
		}
		// The request context may already be cancelled by the time a slow client
		// disconnects; keep its values (the pinned connection) but not its deadline.
		err := rec.Append(context.WithoutCancel(c.Request.Context()), appaudit.Entry{
			TenantID:   tenantID,
			UserID:     middleware.ActorUUID(c),
			Action:     r.action,
			Resource:   r.resource,
			ResourceID: resourceID,
			ClientIP:   c.ClientIP(),
		})
		if err != nil {
			slog.Warn("audit: append failed", slog.String("action", r.action), slog.Any("error", err))
		}
	}
}
