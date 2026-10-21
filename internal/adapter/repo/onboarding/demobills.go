package onboarding

import "fmt"

// DemoBillsSQL is the sub-select (bind $1 = tenant id) of ids of bills that count
// as sample data: every demo bill (remark='DEMO'), every draft/voided/soft-deleted
// bill holding only demo products (e.g. the draft purchase the replenish page
// generates from a demo shortage), and, recursively, every bill that returns
// (link_bill_id) or amends (amendment_of_id) one of those and holds only demo
// products. Approved real bills are never included. ClearDemo deletes exactly
// these bills; the team store uses them to tell a sample-only shop from a used one.
func DemoBillsSQL() string {
	demoOnlyTpl := `NOT EXISTS (SELECT 1 FROM tally.bill_item i JOIN tally.product p ON p.id = i.product_id ` +
		`WHERE i.head_id = %s.id AND p.remark IS DISTINCT FROM 'DEMO')`
	demoOnlyH, demoOnlyC := fmt.Sprintf(demoOnlyTpl, "h"), fmt.Sprintf(demoOnlyTpl, "c")
	return `(WITH RECURSIVE db(id) AS (` +
		`SELECT h.id FROM tally.bill_head h WHERE h.tenant_id = $1 AND (h.remark = 'DEMO' OR ` +
		`((h.status IN (0, 9) OR h.deleted_at IS NOT NULL) ` +
		`AND EXISTS (SELECT 1 FROM tally.bill_item i WHERE i.head_id = h.id) AND ` + demoOnlyH + `)) ` +
		`UNION ` +
		`SELECT c.id FROM tally.bill_head c JOIN db ON c.link_bill_id = db.id OR c.amendment_of_id = db.id ` +
		`WHERE c.tenant_id = $1 AND ` + demoOnlyC + `) SELECT id FROM db)`
}
