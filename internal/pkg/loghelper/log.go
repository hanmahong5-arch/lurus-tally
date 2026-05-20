// Package loghelper provides thin structured-logging helpers on top of log/slog.
// Each function writes a single JSON log line to the global slog.Default() logger,
// automatically injecting "tenant_id" and "request_id" from the context when present.
//
// Context key contract:
//   - tenant_id  is stored by AuthMiddleware as a uuid.UUID under key "tenant_id".
//   - request_id is stored by RequestID middleware as a string under key "request_id".
//
// Both keys are read from a plain context.Context (not a gin.Context) so this
// package remains transport-agnostic and testable without Gin.
package loghelper

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
)

// ctxKey is an unexported type for context keys managed by this package.
type ctxKey string

const (
	ctxTenantID  ctxKey = "tenant_id"
	ctxRequestID ctxKey = "request_id"
)

// WithTenantID returns a child context carrying the tenant UUID.
// The loghelper functions automatically extract and log it.
func WithTenantID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, ctxTenantID, id.String())
}

// WithRequestID returns a child context carrying the request id string.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxRequestID, id)
}

// Info logs an informational event with optional extra fields.
//
// Required fields are written first: ts (from slog), level, event, tenant_id, request_id.
// Caller-supplied fields are appended after.
func Info(ctx context.Context, event string, fields map[string]any) {
	slog.Default().InfoContext(ctx, event, buildAttrs(ctx, fields)...)
}

// Warn logs a warning event with optional extra fields.
func Warn(ctx context.Context, event string, fields map[string]any) {
	slog.Default().WarnContext(ctx, event, buildAttrs(ctx, fields)...)
}

// Error logs an error event. err may be nil (unusual but not fatal).
func Error(ctx context.Context, event string, err error, fields map[string]any) {
	attrs := buildAttrs(ctx, fields)
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	slog.Default().ErrorContext(ctx, event, attrs...)
}

// buildAttrs constructs the slog.Attr slice from context values and caller fields.
func buildAttrs(ctx context.Context, fields map[string]any) []any {
	var attrs []any

	if tid, ok := ctx.Value(ctxTenantID).(string); ok && tid != "" {
		attrs = append(attrs, slog.String("tenant_id", tid))
	}
	if rid, ok := ctx.Value(ctxRequestID).(string); ok && rid != "" {
		attrs = append(attrs, slog.String("request_id", rid))
	}

	for k, v := range fields {
		attrs = append(attrs, slog.Any(k, v))
	}
	return attrs
}
