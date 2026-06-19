// Package privacy serves the platform-facing PIPL §47 erasure cascade endpoint.
// It mirrors the contract tally already calls on newhub, so lurus-platform can
// drive every downstream's erasure through one uniform shape.
package privacy

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Eraser is the app-layer port the handler calls.
type Eraser interface {
	Erase(ctx context.Context, accountID int64) (int, error)
}

// Handler serves POST /internal/v1/privacy/erase.
type Handler struct {
	eraser Eraser
}

// NewHandler constructs the handler.
func NewHandler(eraser Eraser) *Handler { return &Handler{eraser: eraser} }

type eraseRequest struct {
	EventID   string `json:"event_id"`
	AccountID int64  `json:"account_id"`
	Reason    string `json:"reason"`
}

// Erase handles POST /internal/v1/privacy/erase. Body is
// {event_id, account_id, reason}; the Bearer internal key is enforced by the
// route middleware. Responses follow the platform erase contract: 202 on first
// acceptance (data erased), 200 when the account had no tally data or the
// request is a replay. Both are 2xx, so the platform purge cascade treats the
// erasure as done; a 5xx fails the cascade and is recorded 'expired' for
// operator follow-up (erasure is never silently marked complete).
func (h *Handler) Erase(c *gin.Context) {
	var req eraseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	if req.AccountID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account_id_required"})
		return
	}
	n, err := h.eraser.Erase(c.Request.Context(), req.AccountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erase_failed"})
		return
	}
	if n == 0 {
		c.JSON(http.StatusOK, gin.H{"status": "no_data", "account_id": req.AccountID})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"status":           "accepted",
		"account_id":       req.AccountID,
		"tenants_affected": n,
	})
}
