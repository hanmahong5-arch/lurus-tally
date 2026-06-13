package export

import "database/sql"

const (
	// dateLayout formats date-only columns (bill_date).
	dateLayout = "2006-01-02"
	// dateTimeLayout formats timestamp columns (pay_date).
	dateTimeLayout = "2006-01-02 15:04:05"
)

// Option configures an export use case (functional-options pattern). Every
// constructor takes it variadically, so existing callers pass nothing and keep
// the production row-cap defaults.
type Option func(*options)

type options struct {
	rowLimit int
}

// resolve applies opts on top of the package default cap.
func resolve(defaultLimit int, opts []Option) options {
	o := options{rowLimit: defaultLimit}
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

// WithRowLimit overrides the per-export row cap. Exposed primarily so tests can
// drive the truncation path without seeding tens of thousands of rows; a value
// ≤ 0 is ignored (keeps the default).
func WithRowLimit(n int) Option {
	return func(o *options) {
		if n > 0 {
			o.rowLimit = n
		}
	}
}

// formatNullDate renders a nullable timestamp with layout, or "" when the value
// is NULL. Shared by the bill (date) and payment (datetime) exporters.
func formatNullDate(t sql.NullTime, layout string) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(layout)
}
