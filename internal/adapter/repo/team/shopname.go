package team

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// PrintShopName returns the shop name saved in the print settings, empty when
// never set.
func (s *Store) PrintShopName(ctx context.Context, tenantID uuid.UUID) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(settings -> 'print' ->> 'shop_name', '') FROM tally.tenant WHERE id = $1`, tenantID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("team store: print shop name: %w", err)
	}
	return name, nil
}

// ProfileType returns the shop's chosen business type, empty when none is set.
func (s *Store) ProfileType(ctx context.Context, tenantID uuid.UUID) (string, error) {
	var pt string
	err := s.db.QueryRowContext(ctx,
		`SELECT profile_type FROM tally.tenant_profile WHERE tenant_id = $1`, tenantID).Scan(&pt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("team store: profile type: %w", err)
	}
	return pt, nil
}
