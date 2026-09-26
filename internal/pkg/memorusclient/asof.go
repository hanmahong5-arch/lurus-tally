package memorusclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// SearchAsOf is SearchWithFilter over the facts that held at asOf: memorus
// returns only memories written by then and not yet replaced then
// (GET /memories/search?as_of=, memorus 0957310+). An older memorus ignores
// the parameter and answers with current facts — callers that must not show
// current values as history should check the server version first.
func (c *Client) SearchAsOf(ctx context.Context, userID, query string, limit int, metaEq map[string]string, asOf time.Time) ([]Memory, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("as_of", asOf.UTC().Format(time.RFC3339Nano))
	if userID != "" {
		params.Set("user_id", userID)
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	for k, v := range metaEq {
		params.Set("metadata."+k, v)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/memories/search?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: create search request: %s", ErrUnavailable, err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := classifyStatus(resp.StatusCode); err != nil {
		return nil, err
	}

	var envelope struct {
		Results []struct {
			ID        string         `json:"id"`
			Content   string         `json:"content"`
			Memory    string         `json:"memory"`
			UserID    string         `json:"user_id"`
			Score     float64        `json:"score"`
			Metadata  map[string]any `json:"metadata"`
			CreatedAt string         `json:"created_at"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("%w: decode search response: %s", ErrUnavailable, err)
	}
	out := make([]Memory, 0, len(envelope.Results))
	for _, r := range envelope.Results {
		content := r.Content
		if content == "" {
			content = r.Memory
		}
		var createdAt time.Time
		if r.CreatedAt != "" {
			createdAt, _ = time.Parse(time.RFC3339, r.CreatedAt)
		}
		out = append(out, Memory{
			ID:        r.ID,
			UserID:    r.UserID,
			Content:   content,
			Metadata:  r.Metadata,
			Score:     r.Score,
			CreatedAt: createdAt,
		})
	}
	return out, nil
}
