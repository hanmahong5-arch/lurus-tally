package account

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrPinInvalid is returned when the PIN is not 4 to 6 digits.
	ErrPinInvalid = errors.New("account: cashier pin invalid")
	// ErrPinWrong is returned when the PIN does not match the one the owner set.
	ErrPinWrong = errors.New("account: cashier pin wrong")
	// ErrCashierNotRetail is returned when a shop that cannot use the POS tries to turn the mode on.
	ErrCashierNotRetail = errors.New("account: cashier mode needs a retail shop")
	// ErrPinAlreadySet is returned when another device stored the first PIN a moment earlier.
	ErrPinAlreadySet = errors.New("account: cashier pin already set")
	// ErrPinNotSet is returned when a PIN is checked before the owner has set one.
	ErrPinNotSet = errors.New("account: cashier pin not set")
	// ErrPinLocked is returned after too many wrong PIN attempts in a row.
	ErrPinLocked = errors.New("account: cashier pin locked")
)

// CashierRecord is the stored cashier-mode block. Stored under
// tenant.settings -> 'cashier_mode' (JSONB), no migration needed.
// It holds only the PIN: whether a device is in cashier mode is kept on that
// device. Enabled is the legacy shared flag, read from old rows and ignored.
type CashierRecord struct {
	Enabled bool   `json:"enabled,omitempty"`
	PinHash string `json:"pin_hash,omitempty"`
	Salt    string `json:"salt,omitempty"`
}

// CashierState is what the client may see: never the hash.
type CashierState struct {
	HasPin bool `json:"has_pin"`
}

// CashierModeRepo persists the cashier_mode block of tenant.settings.
type CashierModeRepo interface {
	GetCashier(ctx context.Context, tenantID uuid.UUID) (CashierRecord, error)
	PutCashier(ctx context.Context, tenantID uuid.UUID, r CashierRecord) error
	// PutCashierFirst stores the first PIN only if none is stored yet, else ErrPinAlreadySet.
	PutCashierFirst(ctx context.Context, tenantID uuid.UUID, r CashierRecord) error
	// ProfileType returns the shop's business type ("" when not chosen yet).
	ProfileType(ctx context.Context, tenantID uuid.UUID) (string, error)
}

func stateOf(r CashierRecord) CashierState {
	return CashierState{HasPin: r.PinHash != ""}
}

// GetCashierMode reads whether the shop has set a cashier PIN.
type GetCashierMode struct{ repo CashierModeRepo }

// NewGetCashierMode constructs the use case.
func NewGetCashierMode(r CashierModeRepo) *GetCashierMode { return &GetCashierMode{repo: r} }

// Execute returns the client-visible state.
func (uc *GetCashierMode) Execute(ctx context.Context, tenantID uuid.UUID) (CashierState, error) {
	rec, err := uc.repo.GetCashier(ctx, tenantID)
	if err != nil {
		return CashierState{}, err
	}
	return stateOf(rec), nil
}

const (
	pinIterations = 100_000
	pinMaxFails   = 5
	pinLockFor    = 5 * time.Minute
)

// pinKey scopes failure counting to one member of one shop, so a colleague's
// typos can never lock the owner or the counter out.
type pinKey struct {
	tenant uuid.UUID
	actor  string
}

type pinAttempts struct {
	fails int
	until time.Time
	// last is the latest counted attempt; idle entries are swept after pinLockFor.
	last time.Time
}

// SetCashierMode stores the cashier PIN and verifies it. Leaving cashier mode on
// a device goes through Verify.
type SetCashierMode struct {
	repo  CashierModeRepo
	mu    sync.Mutex
	tries map[pinKey]*pinAttempts
	// swept is when expired entries were last dropped from tries.
	swept time.Time
}

// NewSetCashierMode constructs the use case.
func NewSetCashierMode(r CashierModeRepo) *SetCashierMode {
	return &SetCashierMode{repo: r, tries: map[pinKey]*pinAttempts{}}
}

func validPin(p string) bool {
	if len(p) < 4 || len(p) > 6 {
		return false
	}
	for _, c := range p {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func hashPin(pin string, salt []byte) (string, error) {
	k, err := pbkdf2.Key(sha256.New, pin, salt, pinIterations, 32)
	if err != nil {
		return "", fmt.Errorf("account: hash pin: %w", err)
	}
	return hex.EncodeToString(k), nil
}

// sweepLocked drops entries that can no longer matter: lock over, or no attempt
// for pinLockFor. Callers hold mu; it runs at most once a minute.
func (uc *SetCashierMode) sweepLocked(now time.Time) {
	if now.Sub(uc.swept) < time.Minute {
		return
	}
	uc.swept = now
	for k, a := range uc.tries {
		if !now.Before(a.until) && now.Sub(a.last) >= pinLockFor {
			delete(uc.tries, k)
		}
	}
}

// reserve counts one attempt for k before the PIN is compared, in the same
// critical section that checks the lockout, so concurrent guesses cannot all
// slip past a not-yet-updated counter. It reports false while k is locked out.
func (uc *SetCashierMode) reserve(k pinKey, now time.Time) bool {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	uc.sweepLocked(now)
	a := uc.tries[k]
	if a == nil {
		a = &pinAttempts{}
		uc.tries[k] = a
	}
	if now.Before(a.until) {
		return false
	}
	a.last = now
	a.fails++
	if a.fails >= pinMaxFails {
		a.until = now.Add(pinLockFor)
		a.fails = 0
	}
	return true
}

func (uc *SetCashierMode) clearFails(k pinKey) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	delete(uc.tries, k)
}

// checkPin compares the PIN with the stored one. The attempt is counted first
// (see reserve) and forgiven only when the PIN is right.
func (uc *SetCashierMode) checkPin(k pinKey, rec CashierRecord, pin string) error {
	if !uc.reserve(k, time.Now()) {
		return ErrPinLocked
	}
	salt, err := hex.DecodeString(rec.Salt)
	if err != nil {
		return fmt.Errorf("account: stored salt: %w", err)
	}
	h, err := hashPin(pin, salt)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(h), []byte(rec.PinHash)) != 1 {
		return ErrPinWrong
	}
	uc.clearFails(k)
	return nil
}

// Execute stores the shop's cashier PIN the first time; once one is stored the
// given PIN must match it (nothing changes). The on/off flag is not kept here:
// it lives on each device.
func (uc *SetCashierMode) Execute(ctx context.Context, tenantID uuid.UUID, actor, pin string) (CashierState, error) {
	rec, err := uc.repo.GetCashier(ctx, tenantID)
	if err != nil {
		return CashierState{}, err
	}
	if rec.PinHash == "" {
		// Only shops that can reach the POS need a cashier PIN.
		pt, err := uc.repo.ProfileType(ctx, tenantID)
		if err != nil {
			return CashierState{}, err
		}
		if pt != "retail" {
			return CashierState{}, ErrCashierNotRetail
		}
	}
	if !validPin(pin) {
		return CashierState{}, ErrPinInvalid
	}
	if rec.PinHash != "" {
		if err := uc.checkPin(pinKey{tenantID, actor}, rec, pin); err != nil {
			return CashierState{}, err
		}
		return stateOf(rec), nil
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return CashierState{}, fmt.Errorf("account: salt: %w", err)
	}
	h, err := hashPin(pin, salt)
	if err != nil {
		return CashierState{}, err
	}
	// The legacy shared on/off flag is dropped: it is never written again.
	rec = CashierRecord{PinHash: h, Salt: hex.EncodeToString(salt)}
	if err := uc.repo.PutCashierFirst(ctx, tenantID, rec); err != nil {
		return CashierState{}, err
	}
	return stateOf(rec), nil
}

// Verify checks the PIN only (a device leaving cashier mode asks this). Wrong
// guesses share the lockout with Execute, counted per member (actor).
func (uc *SetCashierMode) Verify(ctx context.Context, tenantID uuid.UUID, actor, pin string) error {
	if !validPin(pin) {
		return ErrPinInvalid
	}
	rec, err := uc.repo.GetCashier(ctx, tenantID)
	if err != nil {
		return err
	}
	if rec.PinHash == "" {
		return ErrPinNotSet
	}
	return uc.checkPin(pinKey{tenantID, actor}, rec, pin)
}
