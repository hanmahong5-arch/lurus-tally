package reports

import (
	"context"
	"errors"

	"github.com/google/uuid"

	apprep "github.com/hanmahong5-arch/lurus-tally/internal/app/report"
)

// ErrMonthlyUnsupported is returned when the backing repo cannot compute month-end figures.
var ErrMonthlyUnsupported = errors.New("reports: repo does not support the monthly report")

// Monthly returns the month-end reconciliation for month (YYYY-MM, "" = current month).
// The repo opts in by also implementing report.Repo, so Repo and New stay unchanged.
func (uc *UseCase) Monthly(ctx context.Context, tenantID uuid.UUID, month string) (*apprep.Monthly, error) {
	r, ok := uc.repo.(apprep.Repo)
	if !ok {
		return nil, ErrMonthlyUnsupported
	}
	return apprep.NewService(r).Monthly(ctx, tenantID, month)
}
