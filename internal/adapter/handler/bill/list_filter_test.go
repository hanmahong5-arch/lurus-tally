package bill

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

func TestParseListFilter_Invalid(t *testing.T) {
	me := uuid.New()
	cases := []struct {
		name  string
		query string
		user  uuid.UUID
	}{
		{"date_from bad", "date_from=2026-13-01", me},
		{"date_from garbage", "date_from=abc", me},
		{"date_to bad", "date_to=2026/10/31", me},
		{"from after to", "date_from=2026-11-02&date_to=2026-11-01", me},
		{"status not number", "status=abc", me},
		{"status unknown", "status=5", me},
		{"partner_id bad", "partner_id=not-a-uuid", me},
		{"payment_status bad", "payment_status=weird", me},
		{"payment_status with draft", "payment_status=open&status=0", me},
		{"payment_status with cancelled", "payment_status=paid&status=9", me},
		{"q too long", "q=" + strings.Repeat("a", 65), me},
		{"mine without user", "mine=1", uuid.Nil},
		{"mine bad value", "mine=yes", me},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := url.ParseQuery(tc.query)
			var f appbill.BillListFilter
			if err := parseListFilter(q, tc.user, &f); err == nil {
				t.Fatalf("want error for %q, got nil", tc.query)
			}
		})
	}
}

func TestParseListFilter_DateToExclusiveCST(t *testing.T) {
	q, _ := url.ParseQuery("date_from=2026-10-01&date_to=2026-10-31")
	var f appbill.BillListFilter
	if err := parseListFilter(q, uuid.Nil, &f); err != nil {
		t.Fatalf("parse: %v", err)
	}
	wantFrom := time.Date(2026, 10, 1, 0, 0, 0, 0, time.FixedZone("x", 8*3600))
	wantTo := time.Date(2026, 11, 1, 0, 0, 0, 0, time.FixedZone("x", 8*3600))
	if !f.DateFrom.Equal(wantFrom) {
		t.Errorf("DateFrom = %v, want %v", f.DateFrom, wantFrom)
	}
	if !f.DateTo.Equal(wantTo) {
		t.Errorf("DateTo = %v, want %v", f.DateTo, wantTo)
	}
	if got := f.DateTo.Format(time.RFC3339); got != "2026-11-01T00:00:00+08:00" {
		t.Errorf("DateTo = %s", got)
	}
}

func TestParseListFilter_SameDayAllowed(t *testing.T) {
	q, _ := url.ParseQuery("date_from=2026-10-10&date_to=2026-10-10")
	var f appbill.BillListFilter
	if err := parseListFilter(q, uuid.Nil, &f); err != nil {
		t.Fatalf("same day must be valid: %v", err)
	}
}

func TestParseListFilter_Valid(t *testing.T) {
	me := uuid.New()
	pid := uuid.New()
	q, _ := url.ParseQuery("status=2&payment_status=open&partner_id=" + pid.String() + "&q=%20老张%20&mine=1")
	var f appbill.BillListFilter
	if err := parseListFilter(q, me, &f); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.Status == nil || *f.Status != domain.StatusApproved {
		t.Errorf("Status = %v", f.Status)
	}
	if f.PaymentStatus != "open" || f.Keyword != "老张" {
		t.Errorf("PaymentStatus=%q Keyword=%q", f.PaymentStatus, f.Keyword)
	}
	if f.PartnerID == nil || *f.PartnerID != pid {
		t.Errorf("PartnerID = %v", f.PartnerID)
	}
	if f.CreatorID == nil || *f.CreatorID != me {
		t.Errorf("CreatorID = %v", f.CreatorID)
	}
}

func TestParseListFilter_Empty(t *testing.T) {
	var f appbill.BillListFilter
	if err := parseListFilter(url.Values{}, uuid.Nil, &f); err != nil {
		t.Fatalf("empty: %v", err)
	}
	if f.Status != nil || f.PartnerID != nil || f.DateFrom != nil || f.DateTo != nil || f.CreatorID != nil {
		t.Errorf("expected zero filter, got %+v", f)
	}
}

func TestParseListFilter_Sort(t *testing.T) {
	q, _ := url.ParseQuery("sort=bill_date_asc")
	var f appbill.BillListFilter
	if err := parseListFilter(q, uuid.Nil, &f); err != nil || !f.OldestFirst {
		t.Fatalf("asc: err=%v oldest=%v", err, f.OldestFirst)
	}
	q, _ = url.ParseQuery("sort=whatever")
	if err := parseListFilter(q, uuid.Nil, &appbill.BillListFilter{}); err == nil {
		t.Fatal("unknown sort must fail")
	}
}
