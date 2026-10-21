package account

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
)

var _ appacct.CashierModeRepo = (*PrintSettingsRepo)(nil)

// GetCashier returns the stored cashier_mode block, zero value when never saved.
func (r *PrintSettingsRepo) GetCashier(ctx context.Context, tenantID uuid.UUID) (appacct.CashierRecord, error) {
	const q = `SELECT COALESCE(settings -> 'cashier_mode', '{}'::jsonb) FROM tally.tenant WHERE id = $1`
	var raw []byte
	err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, tenantID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return appacct.CashierRecord{}, appacct.ErrNotFound
	}
	if err != nil {
		return appacct.CashierRecord{}, fmt.Errorf("account repo: cashier mode get: %w", err)
	}
	var rec appacct.CashierRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return appacct.CashierRecord{}, fmt.Errorf("account repo: cashier mode decode: %w", err)
	}
	return rec, nil
}

// PutCashier replaces the cashier_mode block, leaving other settings keys untouched.
func (r *PrintSettingsRepo) PutCashier(ctx context.Context, tenantID uuid.UUID, rec appacct.CashierRecord) error {
	raw, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("account repo: cashier mode encode: %w", err)
	}
	const q = `
		UPDATE tally.tenant
		SET settings = jsonb_set(COALESCE(settings, '{}'::jsonb), '{cashier_mode}', $2::jsonb, true),
		    updated_at = now()
		WHERE id = $1`
	res, err := dbscope.From(ctx, r.db).ExecContext(ctx, q, tenantID, string(raw))
	if err != nil {
		return fmt.Errorf("account repo: cashier mode put: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return appacct.ErrNotFound
	}
	return nil
}

// PutCashierFirst stores the first PIN only while none exists, so two devices
// setting it at the same moment cannot both win.
func (r *PrintSettingsRepo) PutCashierFirst(ctx context.Context, tenantID uuid.UUID, rec appacct.CashierRecord) error {
	raw, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("account repo: cashier mode encode: %w", err)
	}
	const q = `
		UPDATE tally.tenant
		SET settings = jsonb_set(COALESCE(settings, '{}'::jsonb), '{cashier_mode}', $2::jsonb, true),
		    updated_at = now()
		WHERE id = $1 AND settings -> 'cashier_mode' ->> 'pin_hash' IS NULL`
	res, err := dbscope.From(ctx, r.db).ExecContext(ctx, q, tenantID, string(raw))
	if err != nil {
		return fmt.Errorf("account repo: cashier mode put first: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return appacct.ErrPinAlreadySet
	}
	return nil
}

// ProfileType returns the shop's chosen business type, "" when none.
func (r *PrintSettingsRepo) ProfileType(ctx context.Context, tenantID uuid.UUID) (string, error) {
	const q = `SELECT profile_type FROM tally.tenant_profile WHERE tenant_id = $1`
	var pt string
	err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, tenantID).Scan(&pt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("account repo: cashier mode profile: %w", err)
	}
	return pt, nil
}
