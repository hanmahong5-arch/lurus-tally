package invoice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/invoice"
)

const (
	defaultLimit = 20
	maxLimit     = 200
)

// CreateUseCase registers an invoice and links it to approved bills.
type CreateUseCase struct{ repo Repository }

// NewCreateUseCase constructs a CreateUseCase.
func NewCreateUseCase(repo Repository) *CreateUseCase { return &CreateUseCase{repo: repo} }

// billSubTypeFor maps an invoice direction to the bill kind it may cover.
func billSubTypeFor(d domain.Direction) (domainbill.BillSubType, string) {
	if d == domain.DirectionOut {
		return domainbill.BillSubTypeSale, "销售单"
	}
	return domainbill.BillSubTypePurchase, "采购单"
}

// Execute validates in, enforces the linking rules under row locks on the bills
// (same tenant, approved, direction/partner match, cumulative amount within the
// bill total) and inserts the invoice.
func (uc *CreateUseCase) Execute(ctx context.Context, in domain.CreateInput) (*domain.Invoice, error) {
	inv, err := domain.NewInvoice(in, time.Now().UTC())
	if err != nil {
		return nil, newErr(ErrInvalidInput, err.Error())
	}
	err = uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		name, err := uc.repo.PartnerName(ctx, tx, inv.TenantID, inv.PartnerID)
		if errors.Is(err, ErrNotFound) {
			return newErr(ErrRule, "往来单位不存在或已被删除")
		}
		if err != nil {
			return err
		}
		inv.PartnerName = name
		if err := uc.checkLinks(ctx, tx, inv); err != nil {
			return err
		}
		return uc.repo.Insert(ctx, tx, inv)
	})
	if err != nil {
		return nil, fmt.Errorf("invoice create: %w", err)
	}
	return inv, nil
}

func (uc *CreateUseCase) checkLinks(ctx context.Context, tx *sql.Tx, inv *domain.Invoice) error {
	if len(inv.Links) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(inv.Links))
	for i, l := range inv.Links {
		ids[i] = l.BillID
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a].String() < ids[b].String() })
	refs, err := uc.repo.LockBills(ctx, tx, inv.TenantID, ids)
	if err != nil {
		return err
	}
	wantSub, wantName := billSubTypeFor(inv.Direction)
	for i := range inv.Links {
		l := &inv.Links[i]
		b, ok := refs[l.BillID]
		if !ok {
			return newErr(ErrRule, "关联的单据不存在")
		}
		l.BillNo = b.BillNo
		switch {
		case b.Status != domainbill.StatusApproved:
			return newErr(ErrRule, fmt.Sprintf("单据 %s 还没有审核，审核后才能登记发票", b.BillNo))
		case b.SubType != wantSub:
			return newErr(ErrRule, fmt.Sprintf("单据 %s 不是%s，不能关联到这张发票", b.BillNo, wantName))
		case b.PartnerID == nil || *b.PartnerID != inv.PartnerID:
			return newErr(ErrRule, fmt.Sprintf("单据 %s 的往来单位与发票不一致", b.BillNo))
		}
		if b.Currency != "" && b.Currency != "CNY" {
			return newErr(ErrRule, fmt.Sprintf("单据 %s 是外币单据，暂不支持关联发票", b.BillNo))
		}
		// Invoices are issued in whole cents; cut the sub-cent tail off the total.
		remaining := b.TotalAmount.Truncate(2).Sub(b.Invoiced)
		if l.Amount.GreaterThan(remaining) {
			return newErr(ErrRule, fmt.Sprintf("单据 %s 最多还能开票 %s 元，本次关联 %s 元已超出", b.BillNo, decimal.Max(remaining, decimal.Zero).StringFixed(2), l.Amount.StringFixed(2)))
		}
	}
	return nil
}

// GetUseCase loads one invoice with its links.
type GetUseCase struct{ repo Repository }

// NewGetUseCase constructs a GetUseCase.
func NewGetUseCase(repo Repository) *GetUseCase { return &GetUseCase{repo: repo} }

// Execute returns the invoice or ErrNotFound.
func (uc *GetUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) (*domain.Invoice, error) {
	inv, err := uc.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("invoice get: %w", err)
	}
	return inv, nil
}

// ListOutput is the list result with the aggregate over the whole filter.
type ListOutput struct {
	Items   []*domain.Invoice
	Total   int
	Summary domain.Summary
}

// ListUseCase lists invoices with filtering and pagination.
type ListUseCase struct{ repo Repository }

// NewListUseCase constructs a ListUseCase.
func NewListUseCase(repo Repository) *ListUseCase { return &ListUseCase{repo: repo} }

// Execute clamps paging and delegates to the repository.
func (uc *ListUseCase) Execute(ctx context.Context, f domain.ListFilter) (*ListOutput, error) {
	if f.TenantID == uuid.Nil {
		return nil, errors.New("invoice list: tenant_id is required")
	}
	if f.Limit <= 0 {
		f.Limit = defaultLimit
	} else if f.Limit > maxLimit {
		f.Limit = maxLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	items, total, err := uc.repo.List(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("invoice list: %w", err)
	}
	sum, err := uc.repo.Summarize(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("invoice list: summary: %w", err)
	}
	return &ListOutput{Items: items, Total: total, Summary: sum}, nil
}

// VoidUseCase voids an issued invoice.
type VoidUseCase struct{ repo Repository }

// NewVoidUseCase constructs a VoidUseCase.
func NewVoidUseCase(repo Repository) *VoidUseCase { return &VoidUseCase{repo: repo} }

// Execute flips an issued invoice to void; its bill links stop counting as invoiced.
func (uc *VoidUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) (*domain.Invoice, error) {
	err := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		inv, err := uc.repo.GetForUpdate(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if err := inv.CanVoid(); err != nil {
			return newErr(ErrConflict, err.Error())
		}
		ok, err := uc.repo.SetStatus(ctx, tx, tenantID, id, domain.StatusIssued, domain.StatusVoid)
		if err != nil {
			return err
		}
		if !ok {
			return newErr(ErrConflict, "发票状态已变化，请刷新后重试")
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
			return nil, err
		}
		return nil, fmt.Errorf("invoice void: %w", err)
	}
	return uc.repo.GetByID(ctx, tenantID, id)
}

// RedInput carries the red-letter invoice's own registration data.
type RedInput struct {
	TenantID    uuid.UUID
	ID          uuid.UUID // original invoice
	InvoiceNo   string
	InvoiceCode string
	IssueDate   time.Time
	Remark      string
	CreatedBy   *uuid.UUID
}

// RedUseCase strikes an issued invoice with a negative red-letter invoice.
type RedUseCase struct{ repo Repository }

// NewRedUseCase constructs a RedUseCase.
func NewRedUseCase(repo Repository) *RedUseCase { return &RedUseCase{repo: repo} }

// Execute creates the red-letter invoice and flips the original to red in one transaction.
func (uc *RedUseCase) Execute(ctx context.Context, in RedInput) (*domain.Invoice, error) {
	var red *domain.Invoice
	err := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		orig, err := uc.repo.GetForUpdate(ctx, tx, in.TenantID, in.ID)
		if err != nil {
			return err
		}
		if err := orig.CanRed(); err != nil {
			return newErr(ErrConflict, err.Error())
		}
		red, err = domain.NewRed(orig, in.InvoiceNo, in.InvoiceCode, in.IssueDate, in.Remark, in.CreatedBy, time.Now().UTC())
		if err != nil {
			return newErr(ErrInvalidInput, err.Error())
		}
		if err := uc.repo.Insert(ctx, tx, red); err != nil {
			return err
		}
		ok, err := uc.repo.SetStatus(ctx, tx, in.TenantID, in.ID, domain.StatusIssued, domain.StatusRed)
		if err != nil {
			return err
		}
		if !ok {
			return newErr(ErrConflict, "发票状态已变化，请刷新后重试")
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrDuplicateNo) {
			return nil, err
		}
		return nil, fmt.Errorf("invoice red: %w", err)
	}
	return uc.repo.GetByID(ctx, in.TenantID, red.ID)
}
