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

// PrintSettingsRepo persists the 'print' block inside tally.tenant.settings.
type PrintSettingsRepo struct{ db *sql.DB }

// NewPrintSettingsRepo wires the repo to db.
func NewPrintSettingsRepo(db *sql.DB) *PrintSettingsRepo { return &PrintSettingsRepo{db: db} }

var _ appacct.PrintSettingsRepo = (*PrintSettingsRepo)(nil)

// GetPrint returns the stored print block, zero value when never saved.
func (r *PrintSettingsRepo) GetPrint(ctx context.Context, tenantID uuid.UUID) (appacct.PrintSettings, error) {
	const q = `SELECT COALESCE(settings -> 'print', '{}'::jsonb) FROM tally.tenant WHERE id = $1`
	var raw []byte
	err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, tenantID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return appacct.PrintSettings{}, appacct.ErrNotFound
	}
	if err != nil {
		return appacct.PrintSettings{}, fmt.Errorf("account repo: print settings get: %w", err)
	}
	var s appacct.PrintSettings
	if err := json.Unmarshal(raw, &s); err != nil {
		return appacct.PrintSettings{}, fmt.Errorf("account repo: print settings decode: %w", err)
	}
	return s, nil
}

// PutPrint replaces the print block, leaving other settings keys untouched.
func (r *PrintSettingsRepo) PutPrint(ctx context.Context, tenantID uuid.UUID, s appacct.PrintSettings) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("account repo: print settings encode: %w", err)
	}
	const q = `
		UPDATE tally.tenant
		SET settings = jsonb_set(COALESCE(settings, '{}'::jsonb), '{print}', $2::jsonb, true),
		    updated_at = now()
		WHERE id = $1`
	res, err := dbscope.From(ctx, r.db).ExecContext(ctx, q, tenantID, string(raw))
	if err != nil {
		return fmt.Errorf("account repo: print settings put: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return appacct.ErrNotFound
	}
	return nil
}
