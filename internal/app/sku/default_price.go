package sku

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// ErrInvalidPrice is the sentinel behind every price validation failure; the
// message of the wrapping *PriceError is plain Chinese, safe to show a shopkeeper.
var ErrInvalidPrice = errors.New("invalid price")

// PriceError is a price validation failure carrying a user-facing Chinese message.
type PriceError struct{ Msg string }

func (e *PriceError) Error() string { return e.Msg }

// Is makes errors.Is(err, ErrInvalidPrice) true for every PriceError.
func (e *PriceError) Is(target error) bool { return target == ErrInvalidPrice }

func priceErr(msg string) error { return &PriceError{Msg: msg} }

// PriceInput is the raw (string) price triple carried by product create/update.
// nil means "not provided" (untouched). On create a blank string is treated the same;
// on update (ParsePricesForUpdate) a blank string means "clear" (reset to 0).
type PriceInput struct {
	Retail    *string
	Wholesale *string
	Purchase  *string
}

// DefaultPrices is the validated form of PriceInput; nil = not provided.
type DefaultPrices struct {
	Retail    *decimal.Decimal
	Wholesale *decimal.Decimal
	Purchase  *decimal.Decimal
}

// IsEmpty reports whether no price was provided (no SKU write needed).
func (p DefaultPrices) IsEmpty() bool {
	return p.Retail == nil && p.Wholesale == nil && p.Purchase == nil
}

// DefaultSKUWriter upserts the default SKU's prices. Stock is never touched.
type DefaultSKUWriter interface {
	UpsertDefaultSKU(ctx context.Context, tenantID, productID uuid.UUID, prices DefaultPrices) error
}

// ParsePrices validates a PriceInput: blank → not provided, negative or malformed → error.
// Values are rounded to 6 decimals (NUMERIC(18,6)).
func ParsePrices(in PriceInput) (DefaultPrices, error) { return parsePrices(in, false) }

// ParsePricesForUpdate is ParsePrices where a provided-but-blank value clears the
// price (resets it to 0) instead of leaving it untouched; nil still means untouched.
func ParsePricesForUpdate(in PriceInput) (DefaultPrices, error) { return parsePrices(in, true) }

func parsePrices(in PriceInput, blankClears bool) (DefaultPrices, error) {
	var out DefaultPrices
	var err error
	if out.Retail, err = parseOnePrice(in.Retail, "零售价", blankClears); err != nil {
		return DefaultPrices{}, err
	}
	if out.Wholesale, err = parseOnePrice(in.Wholesale, "批发价", blankClears); err != nil {
		return DefaultPrices{}, err
	}
	if out.Purchase, err = parseOnePrice(in.Purchase, "进价", blankClears); err != nil {
		return DefaultPrices{}, err
	}
	return out, nil
}

func parseOnePrice(raw *string, label string, blankClears bool) (*decimal.Decimal, error) {
	if raw == nil {
		return nil, nil
	}
	s := strings.TrimSpace(*raw)
	if s == "" {
		if blankClears {
			z := decimal.Zero
			return &z, nil
		}
		return nil, nil
	}
	d, err := decimalutil.Parse(s, label)
	if err != nil {
		return nil, priceErr(label + "格式不正确，请输入数字")
	}
	if d.IsNegative() {
		return nil, priceErr(label + "不能为负数")
	}
	if d.GreaterThanOrEqual(decimal.New(1, 12)) {
		return nil, priceErr(label + "数值过大")
	}
	d = d.Round(6)
	return &d, nil
}
