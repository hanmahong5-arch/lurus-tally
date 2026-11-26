package importing

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// parseCSV dispatches to the platform-specific CSV parser.
func parseCSV(p Platform, data []byte) ([]OrderRow, error) {
	switch p {
	case PlatformAmazon:
		return parseAmazonCSV(data)
	case PlatformShopify:
		return parseShopifyCSV(data)
	default:
		return nil, fmt.Errorf("importing: no parser for platform %q", string(p))
	}
}

// parseAmazonCSV parses Amazon order CSV bytes into OrderRow records.
func parseAmazonCSV(data []byte) ([]OrderRow, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("amazon csv: read header: %w", err)
	}
	idx, err := columnIndex(header, map[string][]string{
		"order_id":   {"order-id", "order_id", "orderid"},
		"sku":        {"sku", "asin", "listing-sku", "merchant-sku"},
		"qty":        {"quantity-purchased", "quantity", "qty"},
		"unit_price": {"item-price", "unit-price", "price"},
		"currency":   {"currency"},
		"order_date": {"purchase-date", "order-date", "date"},
	})
	if err != nil {
		return nil, fmt.Errorf("amazon csv: %w", err)
	}

	var rows []OrderRow
	lineNo := 1
	for {
		lineNo++
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("amazon csv: line %d: %w", lineNo, err)
		}
		row, err := buildRow(rec, idx)
		if err != nil {
			return nil, fmt.Errorf("amazon csv: line %d: %w", lineNo, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// parseShopifyCSV parses Shopify order CSV bytes into OrderRow records, skipping blank SKU fee lines.
func parseShopifyCSV(data []byte) ([]OrderRow, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("shopify csv: read header: %w", err)
	}
	idx, err := columnIndex(header, map[string][]string{
		"order_id":   {"name", "order_name", "order-name", "order_id", "order-id"},
		"sku":        {"lineitem sku", "lineitem_sku", "sku", "variant sku"},
		"qty":        {"lineitem quantity", "lineitem_quantity", "quantity", "qty"},
		"unit_price": {"lineitem price", "lineitem_price", "price"},
		"currency":   {"currency"},
		"order_date": {"created at", "created_at", "order_date"},
	})
	if err != nil {
		return nil, fmt.Errorf("shopify csv: %w", err)
	}

	var rows []OrderRow
	lineNo := 1
	for {
		lineNo++
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("shopify csv: line %d: %w", lineNo, err)
		}
		if idx["sku"] < len(rec) && strings.TrimSpace(rec[idx["sku"]]) == "" {
			continue
		}
		row, err := buildRow(rec, idx)
		if err != nil {
			return nil, fmt.Errorf("shopify csv: line %d: %w", lineNo, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// columnIndex matches header columns case-insensitively against field alias lists.
func columnIndex(header []string, fields map[string][]string) (map[string]int, error) {
	normalised := make([]string, len(header))
	for i, h := range header {
		normalised[i] = strings.ToLower(strings.TrimSpace(h))
	}

	result := make(map[string]int, len(fields))
	for field, aliases := range fields {
		found := false
		for _, alias := range aliases {
			for i, h := range normalised {
				if h == alias {
					result[field] = i
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("missing required column %q (tried: %s)", field, strings.Join(aliases, ", "))
		}
	}
	return result, nil
}

// buildRow converts a CSV record into an OrderRow using the resolved column index.
func buildRow(rec []string, idx map[string]int) (OrderRow, error) {
	get := func(field string) string {
		i, ok := idx[field]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}

	qty, err := decimal.NewFromString(get("qty"))
	if err != nil || qty.IsNegative() || qty.IsZero() {
		return OrderRow{}, fmt.Errorf("invalid qty %q", get("qty"))
	}

	price, err := decimal.NewFromString(get("unit_price"))
	if err != nil || price.IsNegative() {
		return OrderRow{}, fmt.Errorf("invalid unit_price %q", get("unit_price"))
	}

	var orderDate time.Time
	rawDate := get("order_date")
	for _, layout := range []string{
		time.RFC3339, "2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05", "2006-01-02",
		"01/02/2006", "2006/01/02",
	} {
		if t, err := time.Parse(layout, rawDate); err == nil {
			orderDate = t.UTC()
			break
		}
	}
	if orderDate.IsZero() {
		return OrderRow{}, fmt.Errorf("cannot parse order_date %q", rawDate)
	}

	return OrderRow{
		PlatformOrderNo: get("order_id"),
		PlatformSKU:     get("sku"),
		Qty:             qty,
		UnitPrice:       price,
		Currency:        strings.ToUpper(get("currency")),
		OrderDate:       orderDate,
	}, nil
}
