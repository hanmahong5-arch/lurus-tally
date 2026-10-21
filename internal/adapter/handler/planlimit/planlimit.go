// Package planlimit maps an entitlement plan-limit error to the shared 403 body.
package planlimit

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
)

// Code 是前端识别套餐超限的错误码。
const Code = "plan_limit_reached"

// Write 在 err 是套餐超限错误时写 403 并返回 true，否则不写任何内容返回 false。
func Write(c *gin.Context, err error) bool {
	e, ok := entitlement.IsPlanLimit(err)
	if !ok {
		return false
	}
	c.JSON(http.StatusForbidden, gin.H{
		"code":    Code,
		"error":   Code,
		"message": e.Message(),
		"key":     e.Key,
		"used":    e.Used,
		"limit":   e.Limit,
	})
	return true
}
