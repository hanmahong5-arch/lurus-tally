package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// ErrPrintSettingsInvalid is returned when a print-settings field is too long.
var ErrPrintSettingsInvalid = errors.New("account: print settings invalid")

// ErrPrinterBindFailed is returned when the cloud refuses to bind the printer
// (wrong SN / KEY, or the cloud is unreachable).
var ErrPrinterBindFailed = errors.New("account: printer bind failed")

// PrintSettings is the shop header / footer printed on receipts and delivery
// notes. Stored under tenant.settings -> 'print' (JSONB), no migration needed.
type PrintSettings struct {
	ShopName      string `json:"shop_name"`
	Phone         string `json:"phone"`
	Address       string `json:"address"`
	ReceiptFooter string `json:"receipt_footer"`
	// Printer is the shop's cloud receipt printer; nil when none is set up.
	// Rows written before cloud printing simply lack the key.
	Printer *PrinterSettings `json:"printer,omitempty"`
}

// PrinterSettings is the shop's cloud printer. Key is stored whole (needed to
// bind the device) but only ever echoed back as its last 4 characters.
type PrinterSettings struct {
	SN        string `json:"sn"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	Paper     string `json:"paper"` // "58" or "80"
	AutoPrint bool   `json:"auto_print"`
}

// PrinterBinder registers a printer with the cloud account. Nil means the
// platform has no cloud-print account, so settings are saved without binding.
type PrinterBinder interface {
	Bind(ctx context.Context, sn, key, name string) error
}

// PrintSettingsRepo persists the print block of tenant.settings.
type PrintSettingsRepo interface {
	GetPrint(ctx context.Context, tenantID uuid.UUID) (PrintSettings, error)
	PutPrint(ctx context.Context, tenantID uuid.UUID, s PrintSettings) error
}

const (
	printNameMax    = 64
	printPhoneMax   = 32
	printAddressMax = 128
	printFooterMax  = 200
	printerSNMax    = 32
	printerNameMax  = 32
	printerKeyMax   = 64
)

// maskKey keeps only the last 4 characters of a printer key.
func maskKey(k string) string {
	if k == "" {
		return ""
	}
	r := []rune(k)
	if len(r) <= 4 {
		return strings.Repeat("*", len(r))
	}
	return "****" + string(r[len(r)-4:])
}

// masked returns a copy safe to send to the browser.
func (s PrintSettings) masked() PrintSettings {
	if s.Printer != nil {
		p := *s.Printer
		p.Key = maskKey(p.Key)
		s.Printer = &p
	}
	return s
}

// GetPrintSettings reads the tenant's print settings (zero value when unset).
type GetPrintSettings struct{ repo PrintSettingsRepo }

// NewGetPrintSettings constructs the use case.
func NewGetPrintSettings(r PrintSettingsRepo) *GetPrintSettings { return &GetPrintSettings{repo: r} }

// Execute returns the stored settings with the printer key masked.
func (uc *GetPrintSettings) Execute(ctx context.Context, tenantID uuid.UUID) (PrintSettings, error) {
	s, err := uc.repo.GetPrint(ctx, tenantID)
	if err != nil {
		return PrintSettings{}, err
	}
	return s.masked(), nil
}

// PutPrintSettings validates and replaces the tenant's print settings.
type PutPrintSettings struct {
	repo   PrintSettingsRepo
	binder PrinterBinder
}

// NewPutPrintSettings constructs the use case.
func NewPutPrintSettings(r PrintSettingsRepo) *PutPrintSettings { return &PutPrintSettings{repo: r} }

// WithBinder makes a changed printer SN bind the device in the cloud first.
func (uc *PutPrintSettings) WithBinder(b PrinterBinder) *PutPrintSettings {
	uc.binder = b
	return uc
}

// Execute trims, bounds-checks and persists.
func (uc *PutPrintSettings) Execute(ctx context.Context, tenantID uuid.UUID, in PrintSettings) (PrintSettings, error) {
	in.ShopName = strings.TrimSpace(in.ShopName)
	in.Phone = strings.TrimSpace(in.Phone)
	in.Address = strings.TrimSpace(in.Address)
	in.ReceiptFooter = strings.TrimSpace(in.ReceiptFooter)
	if utf8.RuneCountInString(in.ShopName) > printNameMax ||
		utf8.RuneCountInString(in.Phone) > printPhoneMax ||
		utf8.RuneCountInString(in.Address) > printAddressMax ||
		utf8.RuneCountInString(in.ReceiptFooter) > printFooterMax {
		return PrintSettings{}, ErrPrintSettingsInvalid
	}
	prev, err := uc.repo.GetPrint(ctx, tenantID)
	if err != nil {
		return PrintSettings{}, err
	}
	if err := uc.resolvePrinter(ctx, &in, prev); err != nil {
		return PrintSettings{}, err
	}
	if err := uc.repo.PutPrint(ctx, tenantID, in); err != nil {
		return PrintSettings{}, err
	}
	return in.masked(), nil
}

// resolvePrinter validates the printer block and fills in what the caller left
// out. An absent block keeps the stored printer (older clients that only know
// the shop header must not wipe it); a block with empty SN and KEY clears it;
// a masked or empty KEY keeps the stored one; a changed SN is bound first.
func (uc *PutPrintSettings) resolvePrinter(ctx context.Context, in *PrintSettings, prev PrintSettings) error {
	p := in.Printer
	if p == nil {
		in.Printer = prev.Printer
		return nil
	}
	p.SN = strings.TrimSpace(p.SN)
	p.Key = strings.TrimSpace(p.Key)
	p.Name = strings.TrimSpace(p.Name)
	if p.SN == "" && p.Key == "" {
		in.Printer = nil
		return nil
	}
	if p.SN == "" || len(p.SN) > printerSNMax || !isAlnum(p.SN) ||
		utf8.RuneCountInString(p.Name) > printerNameMax || len(p.Key) > printerKeyMax {
		return ErrPrintSettingsInvalid
	}
	if p.Paper == "" {
		p.Paper = "58"
	}
	if p.Paper != "58" && p.Paper != "80" {
		return ErrPrintSettingsInvalid
	}
	oldSN := ""
	if prev.Printer != nil {
		oldSN = prev.Printer.SN
	}
	if p.Key == "" || strings.HasPrefix(p.Key, "*") {
		// 换了打印机必须重新填 KEY；同一台才能沿用已存的。
		if prev.Printer == nil || prev.Printer.Key == "" || p.SN != oldSN {
			return ErrPrintSettingsInvalid
		}
		p.Key = prev.Printer.Key
	}
	if p.SN != oldSN && uc.binder != nil {
		if err := uc.binder.Bind(ctx, p.SN, p.Key, p.Name); err != nil {
			return fmt.Errorf("%w: %v", ErrPrinterBindFailed, err)
		}
	}
	return nil
}

func isAlnum(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}
