package bill

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ErrBatchOutOfSync means receipt batches claim more than the warehouse has.
// The sale is refused so the two numbers cannot drift further apart.
var ErrBatchOutOfSync = errors.New("bill: receipt batches exceed stock on hand")

// BatchShortError is a sale or a supplier return that asks for more than the receipt has left.
// Remaining and Requested are in the line's own unit.
type BatchShortError struct {
	Remaining decimal.Decimal
	Requested decimal.Decimal
}

func (e *BatchShortError) Error() string {
	return "bill: receipt batch short"
}

// BatchWarehouseError means the receipt the sale is pinned to sits in another warehouse.
type BatchWarehouseError struct{}

func (e *BatchWarehouseError) Error() string {
	return "bill: receipt batch is in another warehouse"
}

// BatchLot is one purchase line still in a warehouse, in base units.
type BatchLot struct {
	ID           uuid.UUID
	SourceItemID uuid.UUID
	WarehouseID  uuid.UUID
	Remaining    decimal.Decimal
	ReceivedAt   time.Time
}

// DrawRequest is one sale line's demand, in base units.
// A non-nil SourceItemID pins the draw to that purchase line.
type DrawRequest struct {
	SaleItemID   uuid.UUID
	QtyBase      decimal.Decimal
	Factor       decimal.Decimal
	SourceItemID uuid.UUID
}

// PlannedDraw is one slice of a sale line taken from a batch.
// A nil BatchID is stock that arrived before receipt batches existed, or an offline oversell.
type PlannedDraw struct {
	SaleItemID uuid.UUID
	BatchID    uuid.UUID
	QtyBase    decimal.Decimal
}

// PlanOpenRemaining is how much of a new receipt is still available to sell.
// Goods that fill a previously negative on-hand are already spoken for.
func PlanOpenRemaining(qtyBase, onHandAfter decimal.Decimal) decimal.Decimal {
	before := onHandAfter.Sub(qtyBase)
	if !before.IsNegative() {
		return qtyBase
	}
	left := qtyBase.Add(before)
	if left.IsNegative() {
		return decimal.Zero
	}
	return left
}

// PlanDraws splits each sale line across receipt batches.
// Pinned lines take only their own batch. Other lines take the oldest stock first:
// whatever was on hand before any batch, then batches by ReceivedAt.
// onHand and every quantity are base units for one product in saleWarehouse.
func PlanDraws(saleWarehouse uuid.UUID, onHand decimal.Decimal, lots []BatchLot, lines []DrawRequest, allowNegative bool) ([]PlannedDraw, map[uuid.UUID]decimal.Decimal, error) {
	rem := make(map[uuid.UUID]decimal.Decimal, len(lots))
	sum := decimal.Zero
	for _, lot := range lots {
		if lot.WarehouseID != saleWarehouse {
			continue
		}
		rem[lot.ID] = lot.Remaining
		sum = sum.Add(lot.Remaining)
	}
	if onHand.LessThan(sum) {
		return nil, nil, ErrBatchOutOfSync
	}
	untracked := onHand.Sub(sum)
	order := make([]int, 0, len(lots))
	for i := range lots {
		if lots[i].WarehouseID == saleWarehouse {
			order = append(order, i)
		}
	}
	for i := 1; i < len(order); i++ {
		j := i
		for j > 0 && lotBefore(lots[order[j]], lots[order[j-1]]) {
			order[j], order[j-1] = order[j-1], order[j]
			j--
		}
	}

	var draws []PlannedDraw
	for _, pass := range []bool{true, false} {
		for _, ln := range lines {
			pinned := ln.SourceItemID != uuid.Nil
			if pinned != pass {
				continue
			}
			if !ln.QtyBase.IsPositive() {
				continue
			}
			lot, found := findLot(lots, ln.SourceItemID)
			if pinned && found {
				if lot.WarehouseID != saleWarehouse {
					return nil, nil, &BatchWarehouseError{}
				}
				if rem[lot.ID].LessThan(ln.QtyBase) {
					return nil, nil, short(ln, rem[lot.ID])
				}
				rem[lot.ID] = rem[lot.ID].Sub(ln.QtyBase)
				draws = append(draws, PlannedDraw{SaleItemID: ln.SaleItemID, BatchID: lot.ID, QtyBase: ln.QtyBase})
				continue
			}
			left := ln.QtyBase
			if untracked.IsPositive() {
				take := decimal.Min(left, untracked)
				draws = append(draws, PlannedDraw{SaleItemID: ln.SaleItemID, QtyBase: take})
				untracked = untracked.Sub(take)
				left = left.Sub(take)
			}
			for _, idx := range order {
				if !left.IsPositive() {
					break
				}
				id := lots[idx].ID
				if !rem[id].IsPositive() {
					continue
				}
				take := decimal.Min(left, rem[id])
				rem[id] = rem[id].Sub(take)
				left = left.Sub(take)
				draws = append(draws, PlannedDraw{SaleItemID: ln.SaleItemID, BatchID: id, QtyBase: take})
			}
			if !left.IsPositive() {
				continue
			}
			if !allowNegative {
				return nil, nil, ErrBatchOutOfSync
			}
			draws = append(draws, PlannedDraw{SaleItemID: ln.SaleItemID, QtyBase: left})
		}
	}
	return draws, rem, nil
}

func lotBefore(a, b BatchLot) bool {
	if a.ReceivedAt.Equal(b.ReceivedAt) {
		return a.ID.String() < b.ID.String()
	}
	return a.ReceivedAt.Before(b.ReceivedAt)
}

func findLot(lots []BatchLot, source uuid.UUID) (BatchLot, bool) {
	if source == uuid.Nil {
		return BatchLot{}, false
	}
	for _, lot := range lots {
		if lot.SourceItemID == source {
			return lot, true
		}
	}
	return BatchLot{}, false
}

func short(ln DrawRequest, remainingBase decimal.Decimal) *BatchShortError {
	factor := ln.Factor
	if !factor.IsPositive() {
		factor = decimal.NewFromInt(1)
	}
	return &BatchShortError{
		Remaining: remainingBase.Div(factor),
		Requested: ln.QtyBase.Div(factor),
	}
}

// DrawRow is one recorded slice of a sale line, newest first when restoring.
type DrawRow struct {
	ID      uuid.UUID
	BatchID uuid.UUID
	QtyBase decimal.Decimal
}

// PlanRestore puts returned goods back onto the batches they left, newest draw first.
// A nil BatchID draw has nothing to add back. qty beyond the recorded draws stays untracked.
func PlanRestore(rows []DrawRow, qty decimal.Decimal) (add map[uuid.UUID]decimal.Decimal, left map[uuid.UUID]decimal.Decimal) {
	add = map[uuid.UUID]decimal.Decimal{}
	left = make(map[uuid.UUID]decimal.Decimal, len(rows))
	for _, row := range rows {
		left[row.ID] = row.QtyBase
	}
	need := qty
	for _, row := range rows {
		if !need.IsPositive() {
			break
		}
		take := decimal.Min(need, left[row.ID])
		if !take.IsPositive() {
			continue
		}
		left[row.ID] = left[row.ID].Sub(take)
		need = need.Sub(take)
		if row.BatchID != uuid.Nil {
			add[row.BatchID] = add[row.BatchID].Add(take)
		}
	}
	return add, left
}
