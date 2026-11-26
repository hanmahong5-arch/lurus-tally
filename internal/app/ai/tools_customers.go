package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// CustomerResolution represents the outcome of resolving a spoken customer name.
type CustomerResolution struct {
	Customer     *CustomerRef
	Ambiguous    []CustomerRef
	ResolvedFrom string
}

// customerRecentPurchases returns a named customer's latest approved sale bills and line items.
func (r *Registry) customerRecentPurchases(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args customerRecentPurchasesArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("customer_recent_purchases: invalid args: %w", err)
	}
	name := strings.TrimSpace(args.Customer)
	if name == "" {
		return "", fmt.Errorf("customer_recent_purchases: customer is required")
	}
	limit := 5
	if args.Limit != nil && *args.Limit > 0 {
		limit = min(*args.Limit, 20)
	}

	res, err := r.ResolveCustomerName(ctx, tenantID, name)
	if err != nil {
		return "", fmt.Errorf("customer_recent_purchases: %w", err)
	}
	if len(res.Ambiguous) > 0 {
		return ambiguousCustomers(res.Ambiguous, "several customers match; ask the user which one before answering")
	}
	chosen, resolvedFrom := res.Customer, res.ResolvedFrom
	if chosen == nil {
		return jsonMarshal(map[string]interface{}{"found": false, "customer": name})
	}

	bills, err := r.saleRepo.ListCustomerSales(ctx, tenantID, *chosen, limit)
	if err != nil {
		return "", fmt.Errorf("customer_recent_purchases: %w", err)
	}
	type line struct {
		Product   string `json:"product"`
		Qty       string `json:"qty"`
		Unit      string `json:"unit,omitempty"`
		UnitPrice string `json:"unit_price"`
		Amount    string `json:"amount"`
	}
	type bill struct {
		BillNo string `json:"bill_no"`
		Date   string `json:"date"`
		Total  string `json:"total"`
		Items  []line `json:"items"`
	}
	outBills := make([]bill, 0, len(bills))
	for _, b := range bills {
		ob := bill{BillNo: b.BillNo, Date: b.BillDate.In(shopZone).Format("2006-01-02"), Total: b.Total.StringFixed(2)}
		for _, l := range b.Lines {
			ob.Items = append(ob.Items, line{
				Product:   l.ProductName,
				Qty:       l.Qty.String(),
				Unit:      l.Unit,
				UnitPrice: l.UnitPrice.StringFixed(2),
				Amount:    l.Amount.StringFixed(2),
			})
		}
		outBills = append(outBills, ob)
	}
	out := map[string]interface{}{
		"found":    true,
		"customer": map[string]string{"name": chosen.Name, "code": chosen.Code},
		"bills":    outBills,
		"count":    len(outBills),
	}
	if resolvedFrom != "" {
		out["resolved_from"] = resolvedFrom
		out["note"] = fmt.Sprintf("%s was taken to mean customer %s (the only customer with that surname); say so in the answer", resolvedFrom, chosen.Name)
	}
	return jsonMarshal(out)
}

// ResolveCustomerName resolves a customer by exact/partial name or honorific surname alias.
func (r *Registry) ResolveCustomerName(ctx context.Context, tenantID uuid.UUID, name string) (CustomerResolution, error) {
	cands, err := r.saleRepo.MatchCustomers(ctx, tenantID, name)
	if err != nil {
		return CustomerResolution{}, err
	}
	chosen, ambiguous := pickCustomer(name, cands)
	if chosen != nil || len(ambiguous) > 0 {
		return CustomerResolution{Customer: chosen, Ambiguous: ambiguous}, nil
	}
	s := aliasSurname(name)
	if s == "" {
		return CustomerResolution{}, nil
	}
	byName, err := r.saleRepo.MatchCustomers(ctx, tenantID, s)
	if err != nil {
		return CustomerResolution{}, err
	}
	switch m := customersWithSurname(byName, s); len(m) {
	case 0:
		return CustomerResolution{}, nil
	case 1:
		return CustomerResolution{Customer: &m[0], ResolvedFrom: name}, nil
	default:
		return CustomerResolution{Ambiguous: m}, nil
	}
}

// ambiguousCustomers serialises candidate CustomerRef records for user disambiguation.
func ambiguousCustomers(cands []CustomerRef, note string) (string, error) {
	out := make([]map[string]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, map[string]string{"name": c.Name, "code": c.Code, "phone": c.Phone})
	}
	return jsonMarshal(map[string]interface{}{
		"ambiguous":  true,
		"candidates": out,
		"note":       note,
	})
}

// pickCustomer selects a single matching customer or returns ambiguous candidates.
func pickCustomer(name string, cands []CustomerRef) (*CustomerRef, []CustomerRef) {
	var exactPartners, walkIns, partners []CustomerRef
	for _, c := range cands {
		exact := strings.EqualFold(strings.TrimSpace(c.Name), name)
		switch {
		case c.ID == uuid.Nil && exact:
			walkIns = append(walkIns, c)
		case c.ID == uuid.Nil:
		case exact:
			exactPartners = append(exactPartners, c)
		default:
			partners = append(partners, c)
		}
	}
	switch {
	case len(exactPartners) == 1:
		return &exactPartners[0], nil
	case len(exactPartners) > 1:
		return nil, exactPartners
	case len(walkIns) > 0:
		return &walkIns[0], nil
	case len(partners) == 1:
		return &partners[0], nil
	case len(partners) > 1:
		return nil, partners
	}
	return nil, nil
}
