package bill

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// Bill kinds accepted by the last-price lookup.
const (
	LastPriceSale     = "sale"
	LastPricePurchase = "purchase"
)

// MaxLastPriceProducts caps how many products one lookup may ask for.
const MaxLastPriceProducts = 200

// ErrInvalidLastPriceType is returned for a type other than sale / purchase.
var ErrInvalidLastPriceType = errors.New("last prices: type must be sale or purchase")

// Where a LastPrice came from.
const (
	LastPriceSourceLast    = "last"
	LastPriceSourceDefault = "default"
)

// LastPrice is the unit price a partner was last billed for a product, or (Source
// "default") the product's default price when there is no history.
type LastPrice struct {
	ProductID uuid.UUID
	// UnitPrice is quoted per UnitName / UnitID, the unit the old line was billed in.
	UnitPrice decimal.Decimal
	UnitID    uuid.UUID
	UnitName  string
	// Factor is how many base units one UnitID equals (1 when the line had no unit or
	// the base unit). UnitPrice / Factor is the price per base unit.
	Factor   decimal.Decimal
	BillDate time.Time
	Source   string
}

// BasePrice is the price per base unit.
func (p LastPrice) BasePrice() decimal.Decimal {
	if p.Factor.Sign() <= 0 || p.Factor.Equal(decimal.NewFromInt(1)) {
		return p.UnitPrice
	}
	return p.UnitPrice.Div(p.Factor)
}

// LastPriceQuery selects the latest approved line per product for one partner.
// SubType is the stored bill sub type (销售 / 采购).
type LastPriceQuery struct {
	TenantID   uuid.UUID
	PartnerID  uuid.UUID
	SubType    string
	ProductIDs []uuid.UUID
}

// LastPriceRepo is the read-side persistence port for the last-price lookup. It is
// separate from BillRepo so the many BillRepo test doubles stay untouched.
type LastPriceRepo interface {
	// LastPrices returns, per product that has history, the latest approved line.
	LastPrices(ctx context.Context, q LastPriceQuery) ([]LastPrice, error)
}

// DefaultPriceReader reads a product's default SKU prices (sale: retail, purchase: purchase).
type DefaultPriceReader interface {
	ListDefaultSKUs(ctx context.Context, tenantID uuid.UUID, productIDs []uuid.UUID) ([]appsku.DefaultSKU, error)
}

// BaseUnitNameReader is an optional capability of the LastPriceRepo: the name of each
// product's base unit, used when a default SKU carries no unit name.
type BaseUnitNameReader interface {
	BaseUnitNames(ctx context.Context, tenantID uuid.UUID, productIDs []uuid.UUID) (map[uuid.UUID]string, error)
}

// LastPricesUseCase answers "what did this partner pay last time".
type LastPricesUseCase struct {
	repo     LastPriceRepo
	defaults DefaultPriceReader
}

// WithDefaultPrices makes products without history fall back to their default price.
func (uc *LastPricesUseCase) WithDefaultPrices(d DefaultPriceReader) *LastPricesUseCase {
	uc.defaults = d
	return uc
}

// NewLastPricesUseCase constructs the use case.
func NewLastPricesUseCase(repo LastPriceRepo) *LastPricesUseCase {
	return &LastPricesUseCase{repo: repo}
}

// Execute returns the latest price per product. Products without history get their
// default price (Source "default") when one is set, else are absent. Without a partner there is no
// history to query, so every product is answered with its default price.
func (uc *LastPricesUseCase) Execute(ctx context.Context, tenantID, partnerID uuid.UUID, kind string, productIDs []uuid.UUID) ([]LastPrice, error) {
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("last prices: tenant_id is required")
	}
	var subType string
	switch kind {
	case LastPriceSale:
		subType = string(domain.BillSubTypeSale)
	case LastPricePurchase:
		subType = string(domain.BillSubTypePurchase)
	default:
		return nil, ErrInvalidLastPriceType
	}
	if len(productIDs) == 0 {
		return []LastPrice{}, nil
	}
	seen := make(map[uuid.UUID]struct{}, len(productIDs))
	uniq := make([]uuid.UUID, 0, len(productIDs))
	for _, id := range productIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	var rows []LastPrice
	if partnerID != uuid.Nil {
		var err error
		rows, err = uc.repo.LastPrices(ctx, LastPriceQuery{TenantID: tenantID, PartnerID: partnerID, SubType: subType, ProductIDs: uniq})
		if err != nil {
			return nil, fmt.Errorf("last prices: %w", err)
		}
	}
	have := make(map[uuid.UUID]struct{}, len(rows))
	for i := range rows {
		rows[i].Source = LastPriceSourceLast
		if rows[i].Factor.Sign() <= 0 {
			rows[i].Factor = decimal.NewFromInt(1)
		}
		have[rows[i].ProductID] = struct{}{}
	}
	if rows == nil {
		rows = []LastPrice{}
	}
	if uc.defaults == nil {
		return rows, nil
	}
	var missing []uuid.UUID
	for _, id := range uniq {
		if _, ok := have[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return rows, nil
	}
	skus, err := uc.defaults.ListDefaultSKUs(ctx, tenantID, missing)
	if err != nil {
		return nil, fmt.Errorf("last prices: default prices: %w", err)
	}
	var baseNames map[uuid.UUID]string
	if r, ok := uc.repo.(BaseUnitNameReader); ok {
		baseNames, err = r.BaseUnitNames(ctx, tenantID, missing)
		if err != nil {
			return nil, fmt.Errorf("last prices: base unit names: %w", err)
		}
	}
	for _, s := range skus {
		price := s.RetailPrice
		if kind == LastPricePurchase {
			price = s.PurchasePrice
		}
		if price.Sign() <= 0 {
			continue
		}
		unitName := s.UnitName
		if unitName == "" {
			unitName = baseNames[s.ProductID]
		}
		rows = append(rows, LastPrice{ProductID: s.ProductID, UnitPrice: price, UnitName: unitName, Factor: decimal.NewFromInt(1), Source: LastPriceSourceDefault})
	}
	return rows, nil
}
