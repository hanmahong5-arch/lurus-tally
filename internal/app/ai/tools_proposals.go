package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	"github.com/shopspring/decimal"
)

// priceActionRe validates price adjustment expressions such as +5%, -10%, or =199.00.
var priceActionRe = regexp.MustCompile(`^([+-]?)\s*(\d+(?:\.\d+)?)\s*%$|^=\s*(\d+(?:\.\d+)?)$`)

// proposePriceChange builds a pending confirmation Plan for bulk product price changes.
func (r *Registry) proposePriceChange(ctx context.Context, tenantID uuid.UUID, argsJSON string) (*domainai.Plan, string, error) {
	var args proposePriceChangeArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, "", fmt.Errorf("propose_price_change: invalid args: %w", err)
	}

	actionText, ok := priceActionText(args.Action)
	if !ok {
		return nil, "", fmt.Errorf("propose_price_change: action %q must look like +5%%, -10%% or =199.00", args.Action)
	}

	products, err := r.productRepo.SearchProducts(ctx, tenantID, args.Filter)
	if err != nil {
		return nil, "", fmt.Errorf("propose_price_change: search: %w", err)
	}
	if len(products) == 0 {
		return noProductsFound(args.Filter)
	}

	samples := make([]domainai.SampleRow, 0, min(len(products), 10))
	for i, p := range products {
		if i >= 10 {
			break
		}
		samples = append(samples, domainai.SampleRow{
			Name:   p.Name,
			Before: "当前售价",
			After:  actionText,
		})
	}

	plan := &domainai.Plan{
		ID:       uuid.New(),
		TenantID: tenantID,
		Type:     domainai.PlanTypePriceChange,
		Status:   domainai.PlanStatusPending,
		Payload: map[string]interface{}{
			"filter": args.Filter,
			"action": args.Action,
		},
		Preview: domainai.PlanPreview{
			Description:   fmt.Sprintf("按「%s」调整 %d 个商品的价格：%s", args.Filter, len(products), actionText),
			AffectedCount: len(products),
			SampleRows:    samples,
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}

	result, err := jsonMarshal(map[string]interface{}{
		"plan_id":               plan.ID.String(),
		"affected_count":        len(products),
		"description":           plan.Preview.Description,
		"requires_confirmation": true,
		"message":               "需要店主确认后才会执行，确认卡片已经展示给店主。",
	})
	return plan, result, err
}

// noProductsFound returns a non-card response when a product filter matches zero products.
func noProductsFound(filter string) (*domainai.Plan, string, error) {
	result, err := jsonMarshal(map[string]interface{}{
		"affected_count": 0,
		"message":        fmt.Sprintf("没找到符合「%s」的商品，没有生成确认卡片，请换个关键词再试。", filter),
	})
	return nil, result, err
}

// stockDeltaText formats a stock delta into Chinese confirmation copy.
func stockDeltaText(delta float64) string {
	if delta < 0 {
		return "减少 " + strconv.FormatFloat(-delta, 'f', -1, 64)
	}
	return "增加 " + strconv.FormatFloat(delta, 'f', -1, 64)
}

// priceActionText formats a validated price action expression into Chinese confirmation copy.
func priceActionText(action string) (string, bool) {
	m := priceActionRe.FindStringSubmatch(strings.TrimSpace(action))
	if m == nil {
		return "", false
	}
	switch {
	case m[3] != "":
		return "改为 " + m[3] + " 元", true
	case m[1] == "+" || m[1] == "":
		return "涨价 " + m[2] + "%", true
	default:
		return "降价 " + m[2] + "%", true
	}
}

// proposeCreatePurchaseDraft builds a pending confirmation Plan for a purchase order draft.
func (r *Registry) proposeCreatePurchaseDraft(ctx context.Context, tenantID uuid.UUID, argsJSON string) (*domainai.Plan, string, error) {
	var args proposeCreatePurchaseDraftArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, "", fmt.Errorf("propose_create_purchase_draft: invalid args: %w", err)
	}

	samples := make([]domainai.SampleRow, 0, len(args.Items))
	for _, it := range args.Items {
		samples = append(samples, domainai.SampleRow{
			Name:   it.ProductName,
			Before: "",
			After:  fmt.Sprintf("数量 %.2f", it.Qty),
		})
	}

	plan := &domainai.Plan{
		ID:       uuid.New(),
		TenantID: tenantID,
		Type:     domainai.PlanTypeCreatePurchase,
		Status:   domainai.PlanStatusPending,
		Payload:  map[string]interface{}{"items": args.Items},
		Preview: domainai.PlanPreview{
			Description:   fmt.Sprintf("新建采购草稿，共 %d 行", len(args.Items)),
			AffectedCount: len(args.Items),
			SampleRows:    samples,
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	attachDraftFlags(plan, r.historyFlags(ctx, tenantID, args.Items))

	result, err := jsonMarshal(map[string]interface{}{
		"plan_id":               plan.ID.String(),
		"item_count":            len(args.Items),
		"description":           plan.Preview.Description,
		"requires_confirmation": true,
		"message":               "需要店主确认后才会执行，确认卡片已经展示给店主。",
	})
	return plan, result, err
}

// historyFlags checks purchase draft items against recent sales volume.
func (r *Registry) historyFlags(ctx context.Context, tenantID uuid.UUID, items []purchaseDraftItem) []DraftFlag {
	if r.saleRepo == nil || len(items) == 0 {
		return nil
	}
	rows, err := r.saleRepo.ListRecentSaleLines(ctx, tenantID, histWindowDays)
	if err != nil {
		return nil
	}
	soldByName := make(map[string]decimal.Decimal, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.ProductName)
		soldByName[name] = soldByName[name].Add(row.Qty)
	}
	return CheckHistoryQty(checkLines(items), soldByName)
}

// proposeBulkStockAdjust builds a pending confirmation Plan for bulk stock quantity adjustments.
func (r *Registry) proposeBulkStockAdjust(ctx context.Context, tenantID uuid.UUID, argsJSON string) (*domainai.Plan, string, error) {
	var args proposeBulkStockAdjustArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, "", fmt.Errorf("propose_bulk_stock_adjust: invalid args: %w", err)
	}

	products, err := r.productRepo.SearchProducts(ctx, tenantID, args.Filter)
	if err != nil {
		return nil, "", fmt.Errorf("propose_bulk_stock_adjust: search: %w", err)
	}
	if len(products) == 0 {
		return noProductsFound(args.Filter)
	}

	samples := make([]domainai.SampleRow, 0, min(len(products), 10))
	for i, p := range products {
		if i >= 10 {
			break
		}
		samples = append(samples, domainai.SampleRow{
			Name:   p.Name,
			Before: "当前库存",
			After:  stockDeltaText(args.Delta),
		})
	}

	plan := &domainai.Plan{
		ID:       uuid.New(),
		TenantID: tenantID,
		Type:     domainai.PlanTypeBulkStockAdjust,
		Status:   domainai.PlanStatusPending,
		Payload: map[string]interface{}{
			"filter": args.Filter,
			"delta":  args.Delta,
		},
		Preview: domainai.PlanPreview{
			Description:   fmt.Sprintf("按「%s」调整 %d 个商品的库存：%s", args.Filter, len(products), stockDeltaText(args.Delta)),
			AffectedCount: len(products),
			SampleRows:    samples,
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}

	result, err := jsonMarshal(map[string]interface{}{
		"plan_id":               plan.ID.String(),
		"affected_count":        len(products),
		"description":           plan.Preview.Description,
		"requires_confirmation": true,
		"message":               "需要店主确认后才会执行，确认卡片已经展示给店主。",
	})
	return plan, result, err
}
