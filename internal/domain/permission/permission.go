// Package permission is the shop permission matrix: which role may do what.
// It is pure data and logic; the HTTP layer enforces it (middleware.RequirePerm
// for whole routes, middleware.HasPerm for field-level trimming).
package permission

import (
	"context"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

// Perm is one capability a role can hold.
type Perm string

const (
	CostView       Perm = "cost.view"       // 成本、进价、毛利、库存金额
	BillRead       Perm = "bill.read"       // 看单据
	BillDraft      Perm = "bill.draft"      // 开采购/销售草稿、改草稿
	BillCheckout   Perm = "bill.checkout"   // 收银快速结账
	BillApprove    Perm = "bill.approve"    // 审核单据（动库存）
	BillCancel     Perm = "bill.cancel"     // 作废、恢复单据
	BillReturn     Perm = "bill.return"     // 退货
	BillReturnOwn  Perm = "bill.return.own" // 退货入口：持 bill.return 的退任意单，其余只能退当天本人开的销售单
	PurchaseView   Perm = "purchase.view"   // 采购单入口：有 cost.view 的看全部，其余只看本人草稿且无单价金额
	PaymentRecord  Perm = "payment.record"  // 记收款、付款、核销
	PaymentRead    Perm = "payment.read"    // 看收付款、应收应付、发票
	InvoiceManage  Perm = "invoice.manage"  // 登记、作废、红冲发票
	ReportView     Perm = "report.view"     // 报表
	ExportStock    Perm = "export.stock"    // 导出库存
	ProductWrite   Perm = "product.write"   // 商品、单位的增删改
	StockAdjust    Perm = "stock.adjust"    // 盘点调整
	CatalogWrite   Perm = "catalog.write"   // 仓库、供应商、苗木字典、项目、汇率
	CustomerWrite  Perm = "customer.write"  // 新建客户
	ReplenishView  Perm = "replenish.view"  // 补货建议
	AIUse          Perm = "ai.use"          // 使用智能助手
	AIExecute      Perm = "ai.execute"      // 确认、撤销助手给出的批量操作
	SettingsManage Perm = "settings.manage" // 店铺设置、导入、示例数据、令牌
	BillingManage  Perm = "billing.manage"  // 套餐与计费
	TeamManage     Perm = "team.manage"     // 成员与邀请
	TokenManage    Perm = "token.manage"    // API 密钥（个人访问令牌）的签发与吊销，仅老板
	AuditView      Perm = "audit.view"      // 查看操作记录（谁审核、谁作废、谁改了什么）
	PrintReceipt   Perm = "print.receipt"   // 云打印小票（收银员可用）
	PrintSettings  Perm = "print.settings"  // 云打印机的绑定与设置
)

var matrix = map[domain.Role]map[Perm]bool{
	domain.RoleOwner: set(
		CostView, BillRead, BillDraft, BillCheckout, BillApprove, BillCancel, BillReturn,
		PaymentRecord, PaymentRead, InvoiceManage, ReportView, ExportStock, ProductWrite,
		StockAdjust, CatalogWrite, CustomerWrite, ReplenishView, AIUse, AIExecute,
		SettingsManage, BillingManage, TeamManage, TokenManage, BillReturnOwn, PurchaseView, AuditView,
		PrintReceipt, PrintSettings,
	),
	domain.RoleManager: set(
		CostView, BillRead, BillDraft, BillCheckout, BillApprove, BillCancel, BillReturn,
		PaymentRecord, PaymentRead, InvoiceManage, ReportView, ExportStock, ProductWrite,
		StockAdjust, CatalogWrite, CustomerWrite, ReplenishView, AIUse, AIExecute,
		SettingsManage, TeamManage, BillReturnOwn, PurchaseView, AuditView,
		PrintReceipt, PrintSettings,
	),
	domain.RoleClerk: set(
		BillRead, BillDraft, ExportStock, ProductWrite, StockAdjust, CatalogWrite,
		CustomerWrite, ReplenishView, AIUse, PurchaseView,
		PrintReceipt,
	),
	domain.RoleCashier: set(
		BillRead, BillCheckout, CustomerWrite, BillReturnOwn,
		PrintReceipt,
	),
	domain.RoleFinance: set(
		CostView, BillRead, PaymentRecord, PaymentRead, InvoiceManage, ReportView,
		ExportStock, ReplenishView, AIUse, PurchaseView, AuditView,
	),
}

func set(ps ...Perm) map[Perm]bool {
	m := make(map[Perm]bool, len(ps))
	for _, p := range ps {
		m[p] = true
	}
	return m
}

// Allowed reports whether role holds perm. An unknown role holds nothing.
func Allowed(role domain.Role, perm Perm) bool {
	return matrix[role][perm]
}

// Perms lists the permissions of a role (unordered copy).
func Perms(role domain.Role) []Perm {
	out := make([]Perm, 0, len(matrix[role]))
	for p := range matrix[role] {
		out = append(out, p)
	}
	return out
}

// Label is the shop-facing Chinese name of a permission, used in refusals.
func (p Perm) Label() string {
	switch p {
	case CostView:
		return "查看成本和毛利"
	case BillDraft:
		return "开单"
	case BillCheckout:
		return "收银结账"
	case BillApprove:
		return "审核单据"
	case BillCancel:
		return "作废单据"
	case BillReturn, BillReturnOwn:
		return "办理退货"
	case PurchaseView:
		return "查看采购单"
	case TokenManage:
		return "管理 API 密钥"
	case PaymentRecord:
		return "记收付款"
	case PaymentRead:
		return "查看收付款和发票"
	case InvoiceManage:
		return "管理发票"
	case ReportView:
		return "查看报表"
	case ExportStock:
		return "导出库存"
	case ProductWrite:
		return "修改商品"
	case StockAdjust:
		return "盘点调整库存"
	case CatalogWrite:
		return "修改仓库、供应商、苗木字典、项目等基础资料"
	case CustomerWrite:
		return "新建客户"
	case ReplenishView:
		return "查看补货建议"
	case AIUse:
		return "使用智能助手"
	case AIExecute:
		return "确认智能助手的批量操作"
	case SettingsManage:
		return "修改店铺设置"
	case BillingManage:
		return "管理套餐和付费"
	case TeamManage:
		return "管理团队成员"
	case PrintReceipt:
		return "打印小票"
	case PrintSettings:
		return "设置云打印机"
	case AuditView:
		return "查看操作记录"
	}
	return "执行这个操作"
}

type costHiddenKey struct{}

// WithCostHidden marks ctx as serving a caller without cost.view, for layers
// that cannot see the HTTP request (the search repository drops purchase bills).
func WithCostHidden(ctx context.Context) context.Context {
	return context.WithValue(ctx, costHiddenKey{}, true)
}

// CostHidden reports whether ctx was marked by WithCostHidden.
func CostHidden(ctx context.Context) bool {
	v, _ := ctx.Value(costHiddenKey{}).(bool)
	return v
}
