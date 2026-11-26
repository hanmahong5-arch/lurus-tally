// Package main writes a SQL script that replaces the local acceptance shops
// on a database whose name is tally. It never opens the destination.
//
//	OUT=/tmp/accept-shops.sql go run ./tests/accept/copytenant
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const dsnPrefix = "host=/var/run/postgresql user=dev sslmode=disable dbname="

var sourceDBs = []string{
	"tally_accept",
	"tally_accept_purchase",
	"tally_accept_sales",
	"tally_accept_money",
}

var skipTables = map[string]bool{
	"schema_migrations":    true,
	"currency":             true,
	"dev_account":          true,
	"dev_org":              true,
	"dev_org_invite":       true,
	"dev_org_member":       true,
	"onboarding_drafts":    true,
	"event_outbox":         true,
	"usage_report_outbox":  true,
	"account_audit_log":    true,
	"audit_log":            true,
	"user_session":         true,
}

type shop struct {
	id, name, db string
}

func main() {
	ctx := context.Background()
	outPath := os.Getenv("OUT")
	out := os.Stdout
	if outPath != "" {
		f, err := os.Create(outPath)
		if err != nil {
			fail(err)
		}
		defer f.Close()
		out = f
	}

	var shops []shop
	var inserts []string
	seen := map[string]bool{}
	for _, name := range sourceDBs {
		db, err := sql.Open("pgx", dsnPrefix+name)
		if err != nil {
			fail(err)
		}
		rows, err := shopsIn(ctx, db)
		if err != nil {
			fail(fmt.Errorf("%s: %w", name, err))
		}
		for _, s := range rows {
			if seen[s.id] {
				continue
			}
			seen[s.id] = true
			s.db = name
			shops = append(shops, s)
			part, err := dumpShop(ctx, db, s.id)
			if err != nil {
				fail(fmt.Errorf("%s %s: %w", name, s.id, err))
			}
			inserts = append(inserts, part...)
		}
		_ = db.Close()
	}
	if len(shops) == 0 {
		fail(fmt.Errorf("no acceptance shops found"))
	}
	if err := writeScript(out, shops, inserts); err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "shops=%d statements=%d\n", len(shops), len(inserts))
}

func shopsIn(ctx context.Context, db *sql.DB) ([]shop, error) {
	rows, err := db.QueryContext(ctx, `SELECT id::text, name FROM tally.tenant ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []shop
	for rows.Next() {
		var s shop
		if err := rows.Scan(&s.id, &s.name); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func dumpShop(ctx context.Context, db *sql.DB, tenantID string) ([]string, error) {
	tables, err := copyTables(ctx, db)
	if err != nil {
		return nil, err
	}
	var out []string
	line, err := dumpTable(ctx, db, "tenant", "id", tenantID)
	if err != nil {
		return nil, err
	}
	out = append(out, line...)
	for _, table := range tables {
		line, err = dumpTable(ctx, db, table, "tenant_id", tenantID)
		if err != nil {
			return nil, err
		}
		out = append(out, line...)
	}
	return out, nil
}

func copyTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'tally' AND c.relkind = 'r'
		  AND EXISTS (
		    SELECT 1 FROM pg_attribute a
		    WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped
		  )
		ORDER BY c.relname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if skipTables[name] {
			continue
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

func dumpTable(ctx context.Context, db *sql.DB, table, key, id string) ([]string, error) {
	cols, err := columns(ctx, db, table)
	if err != nil {
		return nil, err
	}
	quoted := make([]string, len(cols))
	vals := make([]string, len(cols))
	for i, col := range cols {
		quoted[i] = quoteIdent(col)
		vals[i] = "quote_nullable(" + quoteIdent(col) + ")"
	}
	q := fmt.Sprintf(
		`SELECT 'INSERT INTO tally.%s (%s) VALUES (' || concat_ws(', ', %s) || ');' FROM tally.%s WHERE %s = $1::uuid`,
		quoteIdent(table), strings.Join(quoted, ", "), strings.Join(vals, ", "), quoteIdent(table), quoteIdent(key),
	)
	rows, err := db.QueryContext(ctx, q, id)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", table, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}
		out = append(out, line)
	}
	return out, rows.Err()
}

func columns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'tally' AND table_name = $1
		ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("table %s has no columns", table)
	}
	return cols, nil
}

func writeScript(out *os.File, shops []shop, inserts []string) error {
	ids := make([]string, len(shops))
	var names []string
	for i, s := range shops {
		ids[i] = quoteLiteral(s.id)
		names = append(names, fmt.Sprintf("%s %s", s.name, s.id))
	}
	inList := strings.Join(ids, ", ")
	var b strings.Builder
	b.WriteString("-- Acceptance shops only. Refuses any other database name.\n")
	for _, name := range names {
		b.WriteString("-- " + name + "\n")
	}
	b.WriteString("DO $$\nBEGIN\n")
	b.WriteString("  IF current_database() <> 'tally' THEN\n")
	b.WriteString("    RAISE EXCEPTION 'accept copy refuses database %', current_database();\n")
	b.WriteString("  END IF;\n")
	b.WriteString("  IF EXISTS (SELECT 1 FROM tally.tenant WHERE id IN (" + inList + ") AND name NOT IN (" + nameList(shops) + ")) THEN\n")
	b.WriteString("    RAISE EXCEPTION 'acceptance tenant id already belongs to a different shop';\n")
	b.WriteString("  END IF;\n")
	b.WriteString("END $$;\n")
	b.WriteString("BEGIN;\n")
	b.WriteString("SET LOCAL session_replication_role = replica;\n")
	for _, table := range tablesFrom(inserts) {
		fmt.Fprintf(&b, "DELETE FROM tally.%s WHERE tenant_id IN (%s);\n", quoteIdent(table), inList)
	}
	fmt.Fprintf(&b, "DELETE FROM tally.tenant WHERE id IN (%s);\n", inList)
	for _, line := range inserts {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("SET LOCAL session_replication_role = origin;\n")
	b.WriteString("COMMIT;\n")
	_, err := out.WriteString(b.String())
	return err
}

func tablesFrom(lines []string) []string {
	seen := map[string]bool{}
	var tables []string
	for _, line := range lines {
		const prefix = "INSERT INTO tally."
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		name := strings.TrimPrefix(line, prefix)
		name = name[:strings.Index(name, " ")]
		name = strings.Trim(name, `"`)
		if name == "tenant" || seen[name] {
			continue
		}
		seen[name] = true
		tables = append(tables, name)
	}
	return tables
}

func nameList(shops []shop) string {
	parts := make([]string, len(shops))
	for i, s := range shops {
		parts[i] = quoteLiteral(s.name)
	}
	return strings.Join(parts, ", ")
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteLiteral(name string) string {
	return "'" + strings.ReplaceAll(name, "'", "''") + "'"
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
