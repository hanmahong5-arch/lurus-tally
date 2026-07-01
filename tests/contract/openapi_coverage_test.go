// Package contract holds the OpenAPI contract gate for the /api/v1 surface.
//
// It builds the REAL Gin router (router.New with real handler structs, so the
// real RegisterRoutes paths are exercised — not the nil-handler stub mirror in
// router.go, which is known to drift, e.g. it omits POST /ai/plans/:id/revert),
// enumerates every registered /api/v1 route, and asserts the OpenAPI document
// documents exactly that set — no undocumented routes, no phantom operations.
//
// This is a fast unit test (no DB, no Docker): handlers are zero-value structs
// whose RegisterRoutes only register method values, which are never invoked here.
package contract

import (
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/account"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/auth"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/billing"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/currency"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/digest"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/export"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/horticulture"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/importing"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/onboarding"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/payment"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/project"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/replenish"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/reports"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/router"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/search"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/stock"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/supplier"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/warehouse"
)

// specPath is the OpenAPI document under test, relative to this package dir.
const specPath = "../../api/openapi.yaml"

// apiPrefix is the client-facing route group the contract gate enforces. The
// /internal/v1 and /webhooks surfaces are intentionally excluded (operator- and
// machine-facing, not part of the public client contract).
const apiPrefix = "/api/v1"

// buildEngine wires the real router with zero-value handler structs. Every
// handler param is non-nil so the real RegisterRoutes runs (capturing routes the
// nil-stub path would miss); product/unit register inline regardless of nil;
// health/metrics serve /internal/* only and stay nil.
func buildEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return router.New(
		nil,            // health.Handler — /internal/v1/tally/* only
		nil, nil, nil,  // authMW, tenantDBMW, idempotencyMW
		nil,            // product.Handler — registered inline, nil-safe
		nil,            // unit.Handler — registered inline, nil-safe
		&auth.Handler{},
		&auth.PATHandler{},
		&stock.Handler{},
		&bill.Handler{},
		&currency.Handler{},
		&bill.SaleHandler{},
		&payment.Handler{},
		&billing.Handler{},
		&ai.Handler{},
		&horticulture.DictHandler{},
		&project.ProjectHandler{},
		nil, // metrics.MetricsHandler — /internal/v1/metrics only
		&supplier.Handler{},
		&warehouse.Handler{},
		&export.Handler{},
		&account.Handler{},
		&replenish.Handler{},
		&reports.Handler{},
		&search.Handler{},
		&importing.Handler{},
		&digest.Handler{},
		&onboarding.Handler{},
	)
}

// ginToOAS rewrites Gin path params (":id") into OpenAPI templating ("{id}").
func ginToOAS(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		if strings.HasPrefix(seg, ":") {
			parts[i] = "{" + seg[1:] + "}"
		}
		if strings.HasPrefix(seg, "*") {
			parts[i] = "{" + seg[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

// realOperations returns the set of "METHOD /path" for every registered route
// under apiPrefix, in OpenAPI path form.
func realOperations(t *testing.T) map[string]bool {
	t.Helper()
	ops := map[string]bool{}
	for _, ri := range buildEngine().Routes() {
		if !strings.HasPrefix(ri.Path, apiPrefix) {
			continue
		}
		ops[strings.ToUpper(ri.Method)+" "+ginToOAS(ri.Path)] = true
	}
	return ops
}

// specOperations returns the set of "METHOD /path" documented in the spec.
func specOperations(t *testing.T, doc *openapi3.T) map[string]bool {
	t.Helper()
	ops := map[string]bool{}
	if doc.Paths == nil {
		return ops
	}
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			ops[strings.ToUpper(method)+" "+path] = true
		}
	}
	return ops
}

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		t.Fatalf("load OpenAPI spec %s: %v", specPath, err)
	}
	return doc
}

// TestOpenAPISpec_CoversEveryAPIv1Route is the drift gate: spec ⊇ real routes
// and spec ⊆ real routes (exact match on path+method).
func TestOpenAPISpec_CoversEveryAPIv1Route(t *testing.T) {
	real := realOperations(t)
	spec := specOperations(t, loadSpec(t))

	var missing []string
	for op := range real {
		if !spec[op] {
			missing = append(missing, op)
		}
	}
	var phantom []string
	for op := range spec {
		if !real[op] {
			phantom = append(phantom, op)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("OpenAPI spec is MISSING %d /api/v1 operation(s) — add them to %s:\n%s",
			len(missing), specPath, strings.Join(missing, "\n"))
	}
	if len(phantom) > 0 {
		sort.Strings(phantom)
		t.Errorf("OpenAPI spec documents %d PHANTOM operation(s) with no matching route — remove them from %s:\n%s",
			len(phantom), specPath, strings.Join(phantom, "\n"))
	}
}
