package memorusclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ErrUnsupported is returned by GetObject when the server has no object view
// (memorus before contract v2 answers 404 for GET /objects/...). Callers fall
// back to search; nothing about the subject can be inferred from it.
var ErrUnsupported = errors.New("memorus: object view not supported by this server")

// Fact is one stored fact as the object view shows it (memorus contract v2,
// GET /api/v1/objects/{subject_id}). Times are UTC.
type Fact struct {
	ID            string
	Content       string
	SlotValue     string
	Source        string
	Attribution   string
	Alias         string
	ValidFrom     *time.Time
	EffectiveFrom time.Time
	CreatedAt     time.Time
	ValidTo       *time.Time
	SupersededBy  string
	EvidenceCount int
	ConflictWith  []string
}

// SlotView is one attribute of the subject with the values in force.
type SlotView struct {
	Slot string
	// Single: written as a single-valued slot; Current is then the value in
	// force (nil only for an empty slot).
	Single  bool
	Current *Fact
	Values  []Fact
}

// Conflict flags rows of one slot that disagree: Reason is
// "multiple_current" (a single-valued slot has several rows in force) or
// "source_disagreement" (two rows in force were written by different sources
// with different values).
type Conflict struct {
	Slot   string
	IDs    []string
	Reason string
}

// ObjectView is a subject's typed facts grouped by slot. An unknown subject
// is an empty view, never an error.
type ObjectView struct {
	SubjectID  string
	ObjectType string
	AsOf       *time.Time
	Slots      []SlotView
	Untyped    []Fact
	Conflicts  []Conflict
	Truncated  bool
}

// Slot returns the named slot, or nil.
func (v *ObjectView) Slot(name string) *SlotView {
	if v == nil {
		return nil
	}
	for i := range v.Slots {
		if v.Slots[i].Slot == name {
			return &v.Slots[i]
		}
	}
	return nil
}

// wire shapes: memorus serialises times as RFC3339 strings.
type factJSON struct {
	ID            string   `json:"id"`
	Content       string   `json:"content"`
	SlotValue     string   `json:"slot_value"`
	Source        string   `json:"source"`
	Attribution   any      `json:"attribution"`
	Alias         any      `json:"alias"`
	ValidFrom     string   `json:"valid_from"`
	EffectiveFrom string   `json:"effective_from"`
	CreatedAt     string   `json:"created_at"`
	ValidTo       string   `json:"valid_to"`
	SupersededBy  string   `json:"superseded_by"`
	EvidenceCount int      `json:"evidence_count"`
	ConflictWith  []string `json:"conflict_with"`
}

type objectJSON struct {
	SubjectID  string `json:"subject_id"`
	ObjectType string `json:"object_type"`
	AsOf       string `json:"as_of"`
	Slots      []struct {
		Slot    string     `json:"slot"`
		Single  bool       `json:"single"`
		Current *factJSON  `json:"current"`
		Values  []factJSON `json:"values"`
	} `json:"slots"`
	Untyped   []factJSON `json:"untyped"`
	Conflicts []struct {
		Slot   string   `json:"slot"`
		IDs    []string `json:"ids"`
		Reason string   `json:"reason"`
	} `json:"conflicts"`
	Truncated bool `json:"truncated"`
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t.UTC()
}

func parseTimePtr(s string) *time.Time {
	if s == "" {
		return nil
	}
	t := parseTime(s)
	return &t
}

func anyString(v any) string {
	s, _ := v.(string)
	return s
}

func (f factJSON) fact() Fact {
	return Fact{
		ID:            f.ID,
		Content:       f.Content,
		SlotValue:     f.SlotValue,
		Source:        f.Source,
		Attribution:   anyString(f.Attribution),
		Alias:         anyString(f.Alias),
		ValidFrom:     parseTimePtr(f.ValidFrom),
		EffectiveFrom: parseTime(f.EffectiveFrom),
		CreatedAt:     parseTime(f.CreatedAt),
		ValidTo:       parseTimePtr(f.ValidTo),
		SupersededBy:  f.SupersededBy,
		EvidenceCount: f.EvidenceCount,
		ConflictWith:  f.ConflictWith,
	}
}

// GetObject reads subjectID's typed facts as they stand now, or as they stood
// at asOf. A server without the route → ErrUnsupported (404 is the only
// signal an older memorus gives; the route itself never answers 404, an
// unknown subject is an empty view).
func (c *Client) GetObject(ctx context.Context, subjectID string, asOf *time.Time) (*ObjectView, error) {
	u := c.baseURL + "/objects/" + url.PathEscape(subjectID)
	if asOf != nil {
		u += "?as_of=" + url.QueryEscape(asOf.UTC().Format(time.RFC3339Nano))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: create object request: %s", ErrUnavailable, err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, ErrUnsupported
	}
	if err := classifyStatus(resp.StatusCode); err != nil {
		return nil, err
	}

	var raw objectJSON
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: decode object response: %s", ErrUnavailable, err)
	}
	view := &ObjectView{
		SubjectID:  raw.SubjectID,
		ObjectType: raw.ObjectType,
		AsOf:       parseTimePtr(raw.AsOf),
		Truncated:  raw.Truncated,
	}
	for _, s := range raw.Slots {
		sv := SlotView{Slot: s.Slot, Single: s.Single}
		if s.Current != nil {
			f := s.Current.fact()
			sv.Current = &f
		}
		for _, v := range s.Values {
			sv.Values = append(sv.Values, v.fact())
		}
		view.Slots = append(view.Slots, sv)
	}
	for _, f := range raw.Untyped {
		view.Untyped = append(view.Untyped, f.fact())
	}
	for _, cf := range raw.Conflicts {
		view.Conflicts = append(view.Conflicts, Conflict{Slot: cf.Slot, IDs: cf.IDs, Reason: cf.Reason})
	}
	return view, nil
}

// AddOptions are the contract-v2 extras of a write.
type AddOptions struct {
	// ValidFrom: when the fact started to hold, for a backdated statement
	// (「从三月起改月结」). Nil = holds from the moment it is recorded.
	ValidFrom *time.Time
	// ReplacesValue: for a multi-valued slot, the old value this write
	// replaces (matched against the stored rows' slot_value). Empty = none.
	ReplacesValue string
}

// AddResult is what memorus reports about a write.
type AddResult struct {
	ID        string
	CreatedAt time.Time
	// Event: ADD (new row), UPDATE (folded into an existing row) or NOOP
	// (restated a known fact).
	Event string
	// Superseded: ids of the rows this write replaced.
	Superseded []string
	// UnmatchedReplacesValue: ReplacesValue was given but no current row of
	// the slot carries that value — nothing was replaced.
	UnmatchedReplacesValue bool
}

// AddWithOptions is Add with the contract-v2 extras, and it reads the
// response instead of synthesising one. Add itself is unchanged: every
// existing caller keeps its behaviour.
func (c *Client) AddWithOptions(ctx context.Context, userID, content string, meta map[string]any, opts AddOptions) (*AddResult, error) {
	body := map[string]any{
		"content": content,
		"user_id": userID,
	}
	m := make(map[string]any, len(meta)+1)
	for k, v := range meta {
		m[k] = v
	}
	if opts.ReplacesValue != "" {
		m["replaces_value"] = opts.ReplacesValue
	}
	if len(m) > 0 {
		body["metadata"] = m
	}
	if opts.ValidFrom != nil {
		body["valid_from"] = opts.ValidFrom.UTC().Format(time.RFC3339Nano)
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("memorusclient: marshal add request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/memories", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: create request: %s", ErrUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := classifyStatus(resp.StatusCode); err != nil {
		return nil, err
	}
	var raw struct {
		ID                     string   `json:"id"`
		CreatedAt              string   `json:"created_at"`
		Event                  string   `json:"event"`
		Superseded             []string `json:"superseded"`
		UnmatchedReplacesValue bool     `json:"unmatched_replaces_value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: decode add response: %s", ErrUnavailable, err)
	}
	return &AddResult{
		ID:                     raw.ID,
		CreatedAt:              parseTime(raw.CreatedAt),
		Event:                  raw.Event,
		Superseded:             raw.Superseded,
		UnmatchedReplacesValue: raw.UnmatchedReplacesValue,
	}, nil
}
