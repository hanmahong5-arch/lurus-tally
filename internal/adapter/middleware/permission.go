package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/permission"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// CtxKeyShopRole is the Gin context key where ResolveShopRole stores the
// caller's team.Role in the current shop.
const CtxKeyShopRole = "shop_role"

// ErrNotShopMember is what a RoleResolver returns for a person who is not (or
// no longer) a member of the shop they claim.
var ErrNotShopMember = errors.New("middleware: not a member of this shop")

// RoleResolver looks up the person's role in the shop (backed by the platform
// organization membership, cached). It must never widen an unknown answer to
// owner.
type RoleResolver func(ctx context.Context, tenantID uuid.UUID, sub string) (team.Role, error)

// ResolveShopRole puts the caller's shop role into the context. Run it right
// after authentication.
//
//   - no tenant            -> untouched (first-time user before shop setup)
//   - no IdP subject       -> a personal access token / automation call: it acts
//     as a shop manager (everything except billing), never as owner
//   - not a member         -> the tenant is dropped from the context so the
//     caller behaves as a person without a shop (they can still /me and join)
//   - lookup failed        -> 503, nothing is guessed
func ResolveShopRole(resolve RoleResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		if applyShopRole(c, resolve) {
			c.Next()
		}
	}
}

// WithShopRole resolves the shop role and then runs next in the same handler
// slot. Auth middleware calls c.Next() itself, so the role cannot be chained
// behind it; it is composed in front of the next middleware instead (the
// tenant pin), which also needs the possibly-dropped tenant.
func WithShopRole(resolve RoleResolver, next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !applyShopRole(c, resolve) {
			return
		}
		if next != nil {
			next(c)
			return
		}
		c.Next()
	}
}

// applyShopRole reports false when it already answered and aborted.
func applyShopRole(c *gin.Context, resolve RoleResolver) bool {
	tenantID := GetTenantID(c)
	if tenantID == uuid.Nil || resolve == nil {
		return true
	}
	sub := GetIDPSubject(c)
	if sub == "" {
		c.Set(CtxKeyShopRole, team.RoleManager)
		return true
	}
	role, err := resolve(c.Request.Context(), tenantID, sub)
	switch {
	case errors.Is(err, ErrNotShopMember):
		c.Set(CtxKeyTenantID, uuid.Nil)
	case err != nil:
		httperr.Write(c, httperr.New(http.StatusServiceUnavailable, "role_unavailable",
			"暂时无法确认你在店里的权限，请稍后再试", "请稍后再试，一直不行请联系客服"))
		c.Abort()
		return false
	default:
		c.Set(CtxKeyShopRole, role)
	}
	return true
}

// ShopRole returns the caller's role, if ResolveShopRole set one.
func ShopRole(c *gin.Context) (team.Role, bool) {
	v, ok := c.Get(CtxKeyShopRole)
	if !ok {
		return "", false
	}
	r, ok := v.(team.Role)
	return r, ok
}

// HasPerm reports whether the caller holds perm. It is for field-level
// decisions inside handlers (trimming cost, purchase price, margin).
//
// A context with no role at all answers true: real requests always pass
// ResolveShopRole / EnforceRoutePerms first (which refuse a shop-bound caller
// without a role), so a missing role only occurs in a handler called directly
// by a unit test.
func HasPerm(c *gin.Context, perm permission.Perm) bool {
	role, ok := ShopRole(c)
	if !ok {
		return true
	}
	return permission.Allowed(role, perm)
}

// CanViewCost is HasPerm(CostView).
func CanViewCost(c *gin.Context) bool { return HasPerm(c, permission.CostView) }

// MarkCostHidden flags the request context of a caller without cost.view, for
// layers below the handlers that cannot see the role (the ⌘K search repository
// drops purchase bills for them).
func MarkCostHidden() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !CanViewCost(c) {
			c.Request = c.Request.WithContext(permission.WithCostHidden(c.Request.Context()))
		}
		c.Next()
	}
}

// WriteForbidden answers 403 with the Chinese refusal for perm.
func WriteForbidden(c *gin.Context, perm permission.Perm) {
	who := "老板或店长"
	if perm == permission.BillingManage || perm == permission.TokenManage {
		who = "老板"
	}
	httperr.Write(c, httperr.Forbidden("forbidden",
		"你的角色没有“"+perm.Label()+"”的权限，请联系"+who))
	c.Abort()
}

// RequirePerm blocks the request unless the caller's role holds perm.
//
//   - no tenant (not signed in, or not in a shop) -> next; the handler answers 401 itself
//   - shop-bound but no resolved role             -> 403 (fail closed)
func RequirePerm(perm permission.Perm) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !checkPerm(c, perm) {
			WriteForbidden(c, perm)
			return
		}
		c.Next()
	}
}

func checkPerm(c *gin.Context, perm permission.Perm) bool {
	if GetTenantID(c) == uuid.Nil {
		return true
	}
	role, ok := ShopRole(c)
	return ok && permission.Allowed(role, perm)
}

// EnforceRoutePerms applies permission.RoutePerm by the matched route, so one
// table decides every audit / void / return / payment / billing / team gate.
// Mount it once on the /api/v1 group, after authentication.
func EnforceRoutePerms() gin.HandlerFunc {
	return func(c *gin.Context) {
		perm, ok := permission.ForRoute(c.Request.Method, c.FullPath())
		if !ok {
			// A shop-bound caller with no role must not reach unlisted routes
			// either: fail closed on a missing role everywhere.
			if GetTenantID(c) != uuid.Nil {
				if _, has := ShopRole(c); !has {
					WriteForbidden(c, "")
					return
				}
			}
			c.Next()
			return
		}
		if !checkPerm(c, perm) {
			WriteForbidden(c, perm)
			return
		}
		c.Next()
	}
}

// JSONTrimmed writes v as JSON, with every cost/margin key removed unless the
// caller holds cost.view. Handlers use it for responses built from structs
// that carry cost (stock snapshots, movements, reports).
func JSONTrimmed(c *gin.Context, status int, v any) {
	if CanViewCost(c) {
		c.JSON(status, v)
		return
	}
	trimmed, err := permission.StripCostFields(v)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(status, trimmed)
}

// bufferedWriter holds a response back so TrimCostResponse can rewrite it.
type bufferedWriter struct {
	gin.ResponseWriter
	buf    bytes.Buffer
	status int
}

func (w *bufferedWriter) Write(b []byte) (int, error)       { return w.buf.Write(b) }
func (w *bufferedWriter) WriteString(s string) (int, error) { return w.buf.WriteString(s) }
func (w *bufferedWriter) WriteHeader(code int)              { w.status = code }
func (w *bufferedWriter) WriteHeaderNow()                   {}
func (w *bufferedWriter) Status() int                       { return w.status }
func (w *bufferedWriter) Size() int                         { return w.buf.Len() }
func (w *bufferedWriter) Written() bool                     { return w.buf.Len() > 0 || w.status != http.StatusOK }

// TrimCostResponse removes every cost/margin key from the JSON response of the
// listed routes (gin FullPath form), for routes whose handler is not edited
// individually (the weekly summary's estimated purchase amount). Other routes
// and callers with cost.view are untouched and unbuffered.
func TrimCostResponse(fullPaths ...string) gin.HandlerFunc {
	paths := make(map[string]bool, len(fullPaths))
	for _, p := range fullPaths {
		paths[p] = true
	}
	return func(c *gin.Context) {
		if !paths[c.FullPath()] || CanViewCost(c) {
			c.Next()
			return
		}
		rewriteJSON(c, permission.StripGeneric)
	}
}

// TrimTodoSummary trims the to-do summary for roles that lack the permissions
// behind its sections: without cost.view the purchase drafts (purchase totals)
// become an empty section; without payment.read the receivable, collection and
// invoice figures are dropped (the dashboard shows "—" for a missing figure).
func TrimTodoSummary(fullPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.FullPath() != fullPath || (CanViewCost(c) && HasPerm(c, permission.PaymentRead)) {
			c.Next()
			return
		}
		cost, pay := CanViewCost(c), HasPerm(c, permission.PaymentRead)
		empty := func() any { return map[string]any{"count": 0, "items": []any{}} }
		rewriteJSON(c, func(v any) any {
			m, ok := v.(map[string]any)
			if !ok {
				return v
			}
			if !cost {
				m["draft_purchases"] = empty()
			}
			if !pay {
				m["overdue_customers"] = map[string]any{"count": 0, "items": []any{}}
				m["uninvoiced_sales"] = empty()
				if k, ok := m["kpis"].(map[string]any); ok {
					delete(k, "receivable_unpaid")
					delete(k, "today_collected")
					delete(k, "month_uninvoiced")
				}
			}
			// The badge is the sum of the section counts: recompute it so a trimmed
			// section does not leak through a total that still includes it.
			if _, has := m["total"]; has {
				m["total"] = todoTotal(m)
			}
			return m
		})
	}
}

// todoSections are the to-do sections whose counts add up to the badge total.
var todoSections = []string{"draft_purchases", "draft_sales", "low_stock", "overdue_customers", "uninvoiced_sales"}

func todoTotal(m map[string]any) int64 {
	var total int64
	for _, k := range todoSections {
		sec, ok := m[k].(map[string]any)
		if !ok {
			continue
		}
		switch n := sec["count"].(type) {
		case json.Number:
			if v, err := n.Int64(); err == nil {
				total += v
			}
		case float64:
			total += int64(n)
		}
	}
	return total
}

// rewriteJSON runs the rest of the chain with the response held back, then
// writes it with fn applied when it is a 200 JSON body. A body that cannot be
// decoded or re-encoded answers 500 instead of leaking the original.
func rewriteJSON(c *gin.Context, fn func(any) any) {
	orig := c.Writer
	bw := &bufferedWriter{ResponseWriter: orig, status: http.StatusOK}
	c.Writer = bw
	c.Next()
	c.Writer = orig

	body := bw.buf.Bytes()
	if bw.status == http.StatusOK && len(body) > 0 && strings.HasPrefix(orig.Header().Get("Content-Type"), "application/json") {
		var generic any
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber()
		if err := dec.Decode(&generic); err != nil {
			// Never fall back to the untrimmed original: it still carries what fn removes.
			rewriteFailed(c, orig, err)
			return
		}
		out, err := json.Marshal(fn(generic))
		if err != nil {
			rewriteFailed(c, orig, err)
			return
		}
		body = out
		orig.Header().Del("Content-Length")
	}
	orig.WriteHeader(bw.status)
	_, _ = orig.Write(body)
}

// rewriteFailed answers 500 for a body that could not be rewritten. Headers the
// handler already set (validators, download names, a length of the original
// body) described the response being dropped, so they go before the error is
// written.
func rewriteFailed(c *gin.Context, orig gin.ResponseWriter, cause error) {
	h := orig.Header()
	for k := range h {
		if http.CanonicalHeaderKey(k) == "X-Request-Id" {
			continue
		}
		delete(h, k)
	}
	httperr.WriteInternal(c, cause)
}
