// Package audit serves GET /api/v1/audit (the shop operation log) and records
// the writes on bill routes into it.
package audit

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appaudit "github.com/hanmahong5-arch/lurus-tally/internal/app/audit"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// Handler serves the operation log.
type Handler struct {
	list *appaudit.ListUseCase
}

// New creates the handler. A nil use case answers 503.
func New(list *appaudit.ListUseCase) *Handler { return &Handler{list: list} }

// RegisterRoutes mounts GET /audit; the caller applies auth and route permissions.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/audit", h.List)
}

// List answers {items, total}, newest first.
func (h *Handler) List(c *gin.Context) {
	if h == nil || h.list == nil {
		httperr.Write(c, httperr.Unavailable(nil))
		return
	}
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		httperr.Write(c, httperr.New(http.StatusUnauthorized, "unauthorized", "请先登录", "请重新登录后再试"))
		return
	}
	f := appaudit.Filter{Limit: 50}
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			httperr.Write(c, httperr.BadRequest("bad_limit", "每页条数不对", "每页请选 1 到 200 条"))
			return
		}
		f.Limit = n
	}
	if v := c.Query("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			httperr.Write(c, httperr.BadRequest("bad_offset", "翻页位置不对", "请回到第一页重试"))
			return
		}
		f.Offset = n
	}
	if v := c.Query("resource"); v != "" {
		if appaudit.ResourceLabel(v) == "其他" {
			httperr.Write(c, httperr.BadRequest("bad_resource", "没有这种对象类型", "请从列表里选择对象类型"))
			return
		}
		f.Resource = v
	}
	if v := c.Query("resource_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			httperr.Write(c, httperr.BadRequest("bad_resource_id", "单据不对", "请从单据页面进入"))
			return
		}
		f.ResourceID = id
	}
	if v := c.Query("from"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, time.Local)
		if err != nil {
			httperr.Write(c, httperr.BadRequest("bad_from", "开始日期格式不对", "请按 2026-01-31 的格式填写"))
			return
		}
		f.From = &t
	}
	if v := c.Query("to"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, time.Local)
		if err != nil {
			httperr.Write(c, httperr.BadRequest("bad_to", "结束日期格式不对", "请按 2026-01-31 的格式填写"))
			return
		}
		t = t.AddDate(0, 0, 1) // the end day is inclusive
		f.To = &t
	}
	items, total, err := h.list.Execute(c.Request.Context(), tenantID, f)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}
