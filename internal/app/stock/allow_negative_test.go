package stock_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// 离线收银回放：货已经卖出去了，库存不足不能拒单，只能把在手数扣成负数；
// 但默认路径（不带 AllowNegative）必须仍然拒绝超卖。
func TestRecordMovement_AllowNegative_OnlyWhenAsked(t *testing.T) {
	for _, strategy := range []string{"wac", "fifo"} {
		t.Run(strategy, func(t *testing.T) {
			out := func(allow bool) appstock.RecordMovementRequest {
				return appstock.RecordMovementRequest{
					TenantID:      testTenantID,
					ProductID:     testProductID,
					WarehouseID:   testWarehouseID,
					Direction:     domain.DirectionOut,
					Qty:           decimal.NewFromInt(15), // 仓里只有 10
					ReferenceType: domain.RefSale,
					AllowNegative: allow,
				}
			}

			// 默认：拒绝，账面不变。
			repo := newMockRepo(newSnapshotWith(10, 5))
			uc := appstock.NewRecordMovementUseCase(repo, appstock.NewCalculator(stubProfile{strategy}, repo), nil, nil)
			if _, err := uc.Execute(context.Background(), out(false)); !appstock.IsInsufficientStock(err) {
				t.Fatalf("默认路径应返回 InsufficientStockError，got %v", err)
			}
			if got := repo.snapshot.OnHandQty; !got.Equal(decimal.NewFromInt(10)) {
				t.Fatalf("被拒后账面应不变，got %s", got)
			}

			// AllowNegative：放行，在手数 10-15 = -5，成本沿用现有成本。
			repo = newMockRepo(newSnapshotWith(10, 5))
			uc = appstock.NewRecordMovementUseCase(repo, appstock.NewCalculator(stubProfile{strategy}, repo), nil, nil)
			snap, err := uc.Execute(context.Background(), out(true))
			if err != nil {
				t.Fatalf("AllowNegative 应放行，got %v", err)
			}
			if want := decimal.NewFromInt(-5); !snap.OnHandQty.Equal(want) {
				t.Fatalf("在手数 = %s, want %s", snap.OnHandQty, want)
			}
			if want := decimal.NewFromInt(5); !snap.UnitCost.Equal(want) {
				t.Fatalf("成本 = %s, want %s", snap.UnitCost, want)
			}
		})
	}
}

// 负库存后补货入库：成本以入库价为准（不能被负数量拉歪），数量回正。
func TestRecordMovement_ReceiptAfterNegative_ResetsCost(t *testing.T) {
	for _, strategy := range []string{"wac", "fifo"} {
		t.Run(strategy, func(t *testing.T) {
			repo := newMockRepo(newSnapshotWith(10, 5))
			uc := appstock.NewRecordMovementUseCase(repo, appstock.NewCalculator(stubProfile{strategy}, repo), nil, nil)
			if _, err := uc.Execute(context.Background(), appstock.RecordMovementRequest{
				TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
				Direction: domain.DirectionOut, Qty: decimal.NewFromInt(15),
				ReferenceType: domain.RefSale, AllowNegative: true,
			}); err != nil {
				t.Fatal(err)
			}
			snap, err := uc.Execute(context.Background(), appstock.RecordMovementRequest{
				TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
				Direction: domain.DirectionIn, Qty: decimal.NewFromInt(20), UnitCost: decimal.NewFromInt(8),
				ReferenceType: domain.RefPurchase,
			})
			if err != nil {
				t.Fatal(err)
			}
			if want := decimal.NewFromInt(15); !snap.OnHandQty.Equal(want) {
				t.Fatalf("在手数 = %s, want %s", snap.OnHandQty, want)
			}
			if want := decimal.NewFromInt(8); !snap.UnitCost.Equal(want) {
				t.Fatalf("成本 = %s, want %s（欠账期间的旧成本不应参与加权）", snap.UnitCost, want)
			}
		})
	}
}
