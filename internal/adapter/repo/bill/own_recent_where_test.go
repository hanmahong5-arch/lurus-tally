package bill

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

// TestBuildListWhere_OwnRecentApproved verifies the draft-or-recently-approved window
// is one parenthesised OR with the cutoff bound as an argument.
func TestBuildListWhere_OwnRecentApproved(t *testing.T) {
	since := time.Now().Add(-7 * 24 * time.Hour)
	w, args, idx := buildListWhere(appbill.BillListFilter{TenantID: uuid.New(), OwnRecentApprovedSince: &since})
	if !strings.Contains(w, "(b.status = 0 OR (b.status = 2 AND b.approved_at >= $2))") {
		t.Errorf("where = %s", w)
	}
	if len(args) != 2 || idx != 3 {
		t.Errorf("args=%d idx=%d, want 2/3", len(args), idx)
	}
}
