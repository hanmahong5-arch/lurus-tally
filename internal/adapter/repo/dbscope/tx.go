package dbscope

import (
	"context"
	"database/sql"
	"fmt"
)

// txKey is unexported so only this package can read or write the scoped tx.
type txKey struct{}

// InTx runs fn inside one transaction and hands it a context for which From
// returns that transaction, so every repository call made with the context joins
// it without any query change. The transaction begins on the tenant-pinned
// connection when the request pinned one (see BeginTx), so RLS binds its writes.
// Commit on success; rollback on error or panic. When ctx already carries a
// scoped tx, fn simply joins it (no nested transaction).
func InTx(ctx context.Context, fallback *sql.DB, fn func(ctx context.Context) error) error {
	if TxFrom(ctx) != nil {
		return fn(ctx)
	}
	tx, err := BeginTx(ctx, fallback, nil)
	if err != nil {
		return fmt.Errorf("dbscope: begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("dbscope: rollback after error (%v): %w", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("dbscope: commit tx: %w", err)
	}
	return nil
}

// TxFrom returns the transaction opened by InTx for this context, or nil.
// Callers that must hand a *sql.Tx to tx-explicit APIs (stock movements) use it.
func TxFrom(ctx context.Context) *sql.Tx {
	tx, _ := ctx.Value(txKey{}).(*sql.Tx)
	return tx
}
