// Package httperr is the single error contract at Tally's HTTP boundary.
//
// Every handler renders failures through Write, which emits one envelope shape —
// {"error": <code>, "message": <text>, "action"?: <text>} — mirroring the
// hand-rolled bill.errResp and the typed platform.Error model that predate it.
//
// The invariant that makes this a security control, not just tidiness:
//
//	For any 5xx, the response body carries a SAFE, STATIC message. The
//	underlying cause (SQL text, DSNs, driver messages, stack-ish detail) is
//	logged server-side and never serialised to the client.
//
// 4xx errors may echo a client-correctable validation message, because that
// raises the signal the caller needs to fix their request and leaks nothing
// internal.
package httperr

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

// ctxKeyRequestID mirrors middleware.CtxKeyRequestID. Duplicated as a literal so
// this low-level package does not import the adapter layer (layering stays one
// directional: adapter -> pkg, never the reverse).
const ctxKeyRequestID = "request_id"

// Error is the typed HTTP error every handler renders through Write.
//
// Invariant: when Status is 5xx, Message MUST be a safe static string — never
// the text of Internal. Internal travels for server-side logging only.
type Error struct {
	Status   int
	Code     string
	Message  string
	Action   string // optional; omitted from the body when empty
	Internal error  // underlying cause; logged, never serialised
}

// Error implements error with a log-safe one-liner that includes the cause.
func (e *Error) Error() string {
	if e.Internal != nil {
		return fmt.Sprintf("httperr: %d %s: %v", e.Status, e.Code, e.Internal)
	}
	return fmt.Sprintf("httperr: %d %s: %s", e.Status, e.Code, e.Message)
}

// Unwrap exposes the cause to errors.Is/As.
func (e *Error) Unwrap() error { return e.Internal }

// New builds a fully specified error. Use for 4xx where message is a safe,
// client-correctable string.
func New(status int, code, message, action string) *Error {
	return &Error{Status: status, Code: code, Message: message, Action: action}
}

// Wrap is New plus an underlying cause for server-side logging. Use it for a
// 5xx that needs a specific code (e.g. "billing_unavailable") while still
// hiding cause from the client.
func Wrap(status int, code, message, action string, cause error) *Error {
	return &Error{Status: status, Code: code, Message: message, Action: action, Internal: cause}
}

// ---- 5xx constructors (generic, leak-free bodies) --------------------------

// Internal is the catch-all 500: a generic body, the real cause logged.
func Internal(cause error) *Error {
	return &Error{
		Status:   http.StatusInternalServerError,
		Code:     "internal_error",
		Message:  "an internal error occurred",
		Action:   "retry shortly; contact support if it keeps happening",
		Internal: cause,
	}
}

// Unavailable is a 503 for a failed/limited dependency the client may retry.
func Unavailable(cause error) *Error {
	return &Error{
		Status:   http.StatusServiceUnavailable,
		Code:     "service_unavailable",
		Message:  "a downstream service is temporarily unavailable",
		Action:   "retry shortly",
		Internal: cause,
	}
}

// BadGateway is a 502 for an invalid response from an upstream dependency.
func BadGateway(cause error) *Error {
	return &Error{
		Status:   http.StatusBadGateway,
		Code:     "bad_gateway",
		Message:  "a downstream service returned an invalid response",
		Action:   "retry shortly",
		Internal: cause,
	}
}

// ---- 4xx constructors (client-facing messages allowed) ---------------------

// BadRequest is a 400; message may carry validation detail.
func BadRequest(code, message, action string) *Error {
	return New(http.StatusBadRequest, code, message, action)
}

// Unauthorized is a 401.
func Unauthorized(code, message string) *Error {
	return New(http.StatusUnauthorized, code, message, "sign in and retry")
}

// Forbidden is a 403.
func Forbidden(code, message string) *Error {
	return New(http.StatusForbidden, code, message, "")
}

// NotFound is a 404.
func NotFound(code, message string) *Error {
	return New(http.StatusNotFound, code, message, "")
}

// Conflict is a 409.
func Conflict(code, message string) *Error {
	return New(http.StatusConflict, code, message, "")
}

// ---- rendering -------------------------------------------------------------

// Write renders err as the canonical envelope. A *Error renders with its
// fields; any other error is treated as an unclassified 500 (its text is logged
// but never exposed). For every 5xx the underlying cause is logged with request
// context so dropping it from the body loses no diagnostics.
func Write(c *gin.Context, err error) {
	he := AsError(err)

	if he.Status >= 500 {
		cause := he.Internal
		if cause == nil {
			cause = err
		}
		slog.Error("request failed",
			slog.Int("status", he.Status),
			slog.String("code", he.Code),
			slog.String("method", c.Request.Method),
			slog.String("path", c.FullPath()),
			slog.String("request_id", c.GetString(ctxKeyRequestID)),
			slog.Any("error", cause),
		)
	}

	body := gin.H{"error": he.Code, "message": he.Message}
	if he.Action != "" {
		body["action"] = he.Action
	}
	c.JSON(he.Status, body)
}

// WriteInternal renders cause, mapping a recognised client-side database
// constraint violation (e.g. a bad foreign key) to a 4xx and everything else to
// a generic 500. Shorthand for the common "c.JSON(500, …err.Error()…)" sites;
// the constraint mapping means a write referencing a non-existent record no
// longer surfaces as an opaque internal_error.
func WriteInternal(c *gin.Context, cause error) {
	if db := classifyDBError(cause); db != nil {
		Write(c, db)
		return
	}
	Write(c, Internal(cause))
}

// classifyDBError inspects err's chain for a PostgreSQL constraint violation that
// is unambiguously the caller's fault and maps it to a 4xx carrying a SAFE,
// STATIC message — the driver's text (table/column/constraint names, key values)
// is never echoed. Returns nil when err is not such a violation, so callers fall
// back to a generic 500.
//
// Only foreign-key (23503) and unique (23505) violations are mapped. NOT NULL
// (23502) and CHECK (23514) usually mean the server failed to populate a field
// it owns — a server bug — so those stay 500 and remain visible rather than being
// disguised as a client error.
func classifyDBError(err error) *Error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return nil
	}
	switch pg.Code {
	case "23503": // foreign_key_violation
		return Conflict("invalid_reference", "the request references a record that does not exist or is still in use")
	case "23505": // unique_violation
		return Conflict("duplicate", "a record with these values already exists")
	default:
		return nil
	}
}

// WriteUnavailable classifies cause as a 503 and writes it.
func WriteUnavailable(c *gin.Context, cause error) { Write(c, Unavailable(cause)) }

// AsError coerces err into a *Error: returns it unchanged if it already is (or
// wraps) one; otherwise maps a recognised DB constraint violation to a 4xx, and
// failing that classifies it as a generic 500.
func AsError(err error) *Error {
	var he *Error
	if errors.As(err, &he) {
		return he
	}
	if db := classifyDBError(err); db != nil {
		return db
	}
	return Internal(err)
}
