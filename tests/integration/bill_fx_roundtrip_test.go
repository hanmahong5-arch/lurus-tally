//go:build integration

// Package integration — bill_fx_roundtrip_test verifies the P0 FX fix end-to-end
// against a real PostgreSQL schema.
//
// Run with:
//
//	go test -v -tags integration -timeout 180s ./tests/integration/ -run TestBillFX
package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	billrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// TestBillFX_ExchangeRatePersistsAndReadsBack proves the persistence half of the FX
// money fix against real Postgres: a foreign-currency purchase bill created through the
// real bill repo stores its exchange_rate, and GetBillForUpdate — the row the approval
// path locks and reads to convert each unit price to base currency — reads the same rate
// back. Before the fix, CreateBill never wrote exchange_rate and scanBillHead never
// selected it, so a DB-loaded head always carried the column default (rate 1) and the
// approval-time FX conversion was inert (USD costs were recorded un-converted). The
// arithmetic half (loaded rate r -> recorded unit_cost == price*r) is covered by the
// unit test TestApprovePurchase_ForeignCurrency_ConvertsUnitCostToBase; together they
// close the chain.
func TestBillFX_ExchangeRatePersistsAndReadsBack(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()

	ctx := context.Background()
	tenantID := insertTenant(t, db, ctx)
	warehouseID := insertWarehouse(t, db, ctx, tenantID)
	productID := insertProduct(t, db, ctx, tenantID, "FX Product", "FX-001")

	repo := billrepo.New(db)

	cases := []struct {
		name     string
		currency string
		rate     decimal.Decimal
	}{
		{"usd_rate_7_2", "USD", decimal.NewFromFloat(7.2)},
		{"cny_rate_1", "CNY", decimal.NewFromInt(1)},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			billID := uuid.New()
			now := time.Now().UTC()
			whID := warehouseID
			head := &domain.BillHead{
				ID:              billID,
				TenantID:        tenantID,
				BillNo:          "PO-FX-" + billID.String()[:8],
				BillType:        domain.BillTypePurchase,
				SubType:         domain.BillSubTypePurchase,
				Status:          domain.StatusDraft,
				WarehouseID:     &whID,
				CreatorID:       uuid.New(),
				BillDate:        now,
				Subtotal:        decimal.NewFromInt(10),
				ShippingFee:     decimal.Zero,
				TaxAmount:       decimal.Zero,
				TotalAmount:     decimal.NewFromInt(10).Mul(tc.rate),
				Currency:        tc.currency,
				ExchangeRateVal: tc.rate,
				AmountLocal:     decimal.NewFromInt(10),
				CreatedAt:       now,
				UpdatedAt:       now,
			}
			item := &domain.BillItem{
				ID:         uuid.New(),
				TenantID:   tenantID,
				HeadID:     billID,
				ProductID:  productID,
				LineNo:     1,
				Qty:        decimal.NewFromInt(1),
				UnitPrice:  decimal.NewFromInt(10),
				LineAmount: decimal.NewFromInt(10),
			}

			if err := repo.WithTx(ctx, func(tx *sql.Tx) error {
				return repo.CreateBill(ctx, tx, head, []*domain.BillItem{item})
			}); err != nil {
				t.Fatalf("CreateBill: %v", err)
			}

			var got *domain.BillHead
			if err := repo.WithTx(ctx, func(tx *sql.Tx) error {
				h, err := repo.GetBillForUpdate(ctx, tx, tenantID, billID)
				if err != nil {
					return err
				}
				got = h
				return nil
			}); err != nil {
				t.Fatalf("GetBillForUpdate: %v", err)
			}

			if !got.ExchangeRateVal.Equal(tc.rate) {
				t.Fatalf("exchange_rate round-trip = %s, want %s — FX conversion would be inert",
					got.ExchangeRateVal, tc.rate)
			}
			t.Logf("PASS: %s exchange_rate persisted and read back as %s", tc.currency, got.ExchangeRateVal)
		})
	}
}
