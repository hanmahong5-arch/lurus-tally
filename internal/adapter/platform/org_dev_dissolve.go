package platform

import (
	"context"
	"fmt"

	appteam "github.com/hanmahong5-arch/lurus-tally/internal/app/team"
)

var _ appteam.OrgDissolver = (*DevOrgClient)(nil)

// DissolveOrg empties an abandoned shop's stand-in organization: pending invites
// are revoked and the actor, if still the only member, leaves. The dev_org row
// stays so (product, external_ref) keeps resolving to the same id.
func (d *DevOrgClient) DissolveOrg(ctx context.Context, orgID, actorAccountID int64) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("dev org: dissolve begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE tally.dev_org_invite SET revoked_at = now()
		 WHERE org_id = $1 AND revoked_at IS NULL AND accepted_at IS NULL`, orgID); err != nil {
		return fmt.Errorf("dev org: dissolve invites: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM tally.dev_org_member
		 WHERE org_id = $1 AND account_id = $2
		   AND (SELECT count(*) FROM tally.dev_org_member WHERE org_id = $1) = 1`, orgID, actorAccountID); err != nil {
		return fmt.Errorf("dev org: dissolve member: %w", err)
	}
	return tx.Commit()
}
