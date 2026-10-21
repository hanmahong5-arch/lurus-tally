// Package print is the cloud receipt-printer port. The shop's printer is
// driven through the vendor's cloud API, so the server never talks to the
// device directly.
package print

import "context"

// PrinterStatus is what the cloud reports about one printer.
type PrinterStatus struct {
	// Online is true when the device is connected to the cloud.
	Online bool
	// Normal is true when it is online and ready (not out of paper, cover open, ...).
	Normal bool
	// Text is the vendor's status sentence, shown to the shop as is.
	Text string
}

// Printer sends receipts to a cloud printer. A nil Printer means the platform
// has no cloud-print account configured; callers must nil-check it.
type Printer interface {
	// Print queues content on the printer sn, times copies, and returns the vendor order id.
	Print(ctx context.Context, sn string, content string, times int) (orderID string, err error)
	// Status asks whether the printer sn is online and ready.
	Status(ctx context.Context, sn string) (PrinterStatus, error)
	// Bind registers a printer (sn + key printed on its label) under the platform account.
	Bind(ctx context.Context, sn, key, name string) error
}
