package permission

// RoutePerm maps "METHOD /full/path" (gin FullPath form) to the permission it
// needs. Routes that are absent are open to every member of the shop; reading
// is open unless the data itself is sensitive.
//
// Keep sensitive new routes here: the router test lists the routes that must
// stay gated.
var RoutePerm = map[string]Perm{
	// Purchase bills: their lines are purchase prices. purchase.view lets a clerk
	// in, but without cost.view the handler narrows the read to the caller's own
	// drafts and drops every price and amount.
	"GET /api/v1/purchase-bills":              PurchaseView,
	"GET /api/v1/purchase-bills/:id":          PurchaseView,
	"POST /api/v1/purchase-bills":             BillDraft,
	"PUT /api/v1/purchase-bills/:id":          BillDraft,
	"POST /api/v1/purchase-bills/:id/approve": BillApprove,
	"POST /api/v1/purchase-bills/:id/cancel":  BillCancel,
	"POST /api/v1/purchase-bills/:id/restore": BillCancel,
	"POST /api/v1/purchase-bills/:id/return":  BillReturn,
	"GET /api/v1/receivables":                 PaymentRead,
	// Sale bills.
	"POST /api/v1/sale-bills/quick-checkout": BillCheckout,
	"POST /api/v1/sale-bills":                BillDraft,
	"PUT /api/v1/sale-bills/:id":             BillDraft,
	"POST /api/v1/sale-bills/:id/approve":    BillApprove,
	"POST /api/v1/sale-bills/:id/cancel":     BillCancel,
	"POST /api/v1/sale-bills/:id/restore":    BillCancel,
	// Return: bill.return.own is the door; without bill.return the handler only
	// lets the caller return a sale they rang up themselves today.
	"POST /api/v1/sale-bills/:id/return": BillReturnOwn,
	// Money.
	"POST /api/v1/payments":              PaymentRecord,
	"GET /api/v1/payments":               PaymentRead,
	"GET /api/v1/receipts/preview":       PaymentRecord,
	"POST /api/v1/receipts":              PaymentRecord,
	"GET /api/v1/partners/:id/statement": PaymentRead,
	"GET /api/v1/invoices":               PaymentRead,
	"GET /api/v1/invoices/:id":           PaymentRead,
	"POST /api/v1/invoices":              InvoiceManage,
	"POST /api/v1/invoices/:id/void":     InvoiceManage,
	"POST /api/v1/invoices/:id/red":      InvoiceManage,
	// Products, units, stock.
	"POST /api/v1/products":             ProductWrite,
	"PUT /api/v1/products/:id":          ProductWrite,
	"DELETE /api/v1/products/:id":       ProductWrite,
	"POST /api/v1/products/:id/restore": ProductWrite,
	"POST /api/v1/products/import":      ProductWrite,
	"POST /api/v1/units":                ProductWrite,
	"DELETE /api/v1/units/:id":          ProductWrite,
	"POST /api/v1/stock/adjustments":    StockAdjust,
	// Basic data.
	"POST /api/v1/warehouses":               CatalogWrite,
	"PUT /api/v1/warehouses/:id":            CatalogWrite,
	"DELETE /api/v1/warehouses/:id":         CatalogWrite,
	"POST /api/v1/warehouses/:id/restore":   CatalogWrite,
	"POST /api/v1/suppliers":                CatalogWrite,
	"PUT /api/v1/suppliers/:id":             CatalogWrite,
	"DELETE /api/v1/suppliers/:id":          CatalogWrite,
	"POST /api/v1/suppliers/:id/restore":    CatalogWrite,
	"POST /api/v1/suppliers/import":         CatalogWrite,
	"POST /api/v1/customers/import":         CatalogWrite,
	"POST /api/v1/nursery-dict":             CatalogWrite,
	"PUT /api/v1/nursery-dict/:id":          CatalogWrite,
	"DELETE /api/v1/nursery-dict/:id":       CatalogWrite,
	"POST /api/v1/nursery-dict/:id/restore": CatalogWrite,
	"POST /api/v1/projects":                 CatalogWrite,
	"PUT /api/v1/projects/:id":              CatalogWrite,
	"DELETE /api/v1/projects/:id":           CatalogWrite,
	"POST /api/v1/projects/:id/restore":     CatalogWrite,
	"POST /api/v1/exchange-rates":           CatalogWrite,
	"POST /api/v1/customers":                CustomerWrite,
	// Reports and exports.
	"GET /api/v1/reports/gross-margin":   CostView,
	"GET /api/v1/reports/abc":            ReportView,
	"GET /api/v1/reports/dead-stock":     ReportView,
	"GET /api/v1/reports/sales-top":      ReportView,
	"GET /api/v1/reports/monthly":        ReportView,
	"GET /api/v1/reports/monthly.csv":    ReportView,
	"GET /api/v1/exports/bills.csv":      ReportView,
	"GET /api/v1/exports/payments.csv":   PaymentRead,
	"GET /api/v1/exports/stock.csv":      ExportStock,
	"GET /api/v1/exports/all.zip":        ReportView,
	"GET /api/v1/replenish/suggestions":  ReplenishView,
	"GET /api/v1/replenish/scorecard":    ReplenishView,
	"POST /api/v1/replenish/draft-batch": BillDraft,
	// Assistant.
	"POST /api/v1/ai/chat":                   AIUse,
	"GET /api/v1/ai/plans":                   AIUse,
	"POST /api/v1/ai/plans/:plan_id/confirm": AIExecute,
	"POST /api/v1/ai/plans/:plan_id/cancel":  AIExecute,
	"POST /api/v1/ai/plans/:plan_id/revert":  AIExecute,
	// Shop settings.
	"PUT /api/v1/tenant/print-settings":  SettingsManage,
	"POST /api/v1/print/receipts":        PrintReceipt,
	"GET /api/v1/tenant/printer/status":  PrintReceipt,
	"POST /api/v1/tenant/printer/test":   PrintSettings,
	"PUT /api/v1/tenant/cashier-mode":    SettingsManage,
	"POST /api/v1/onboarding/seed-demo":  SettingsManage,
	"POST /api/v1/onboarding/clear-demo": SettingsManage,
	"POST /api/v1/imports/orders":        SettingsManage,
	"GET /api/v1/imports/mappings":       SettingsManage,
	"GET /api/v1/account/audit-log":      SettingsManage,
	"POST /api/v1/auth/pats":             TokenManage,
	"GET /api/v1/auth/pats":              TokenManage,
	"DELETE /api/v1/auth/pats/:id":       TokenManage,
	"POST /api/v1/shopify/shops":         SettingsManage,
	"GET /api/v1/shopify/shops":          SettingsManage,
	"DELETE /api/v1/shopify/shops/:id":   SettingsManage,
	// Operation log: owner, manager and finance.
	"GET /api/v1/audit": AuditView,
	// Billing: owner only.
	"GET /api/v1/billing/overview":         BillingManage,
	"GET /api/v1/billing/orders/:order_no": BillingManage,
	"POST /api/v1/billing/subscribe":       BillingManage,
	// Team: owner and manager (joining is open to any signed-in person).
	"GET /api/v1/team/members":                TeamManage,
	"PATCH /api/v1/team/members/:account_id":  TeamManage,
	"DELETE /api/v1/team/members/:account_id": TeamManage,
	"POST /api/v1/team/invites":               TeamManage,
	"GET /api/v1/team/invites":                TeamManage,
	"DELETE /api/v1/team/invites/:id":         TeamManage,
}

// ForRoute returns the permission a route needs.
func ForRoute(method, fullPath string) (Perm, bool) {
	p, ok := RoutePerm[method+" "+fullPath]
	return p, ok
}
