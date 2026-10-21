// Package team implements the /api/v1/team HTTP handlers: members, invites and
// joining a shop by invite link. Management is owner/manager only; joining is
// open to any signed-in person.
package team

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/planlimit"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appteam "github.com/hanmahong5-arch/lurus-tally/internal/app/team"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// Service is the team use-case surface the handler needs (*appteam.Service).
type Service interface {
	ListMembers(ctx context.Context, c appteam.Caller) ([]appteam.MemberView, error)
	CashierAllowed(ctx context.Context, tenantID uuid.UUID) bool
	UpdateMemberRole(ctx context.Context, c appteam.Caller, targetAccountID int64, role domain.Role) error
	RemoveMember(ctx context.Context, c appteam.Caller, targetAccountID int64) error
	CreateInvite(ctx context.Context, c appteam.Caller, role domain.Role, ttlHours int) (*appteam.InviteCreated, error)
	ListInvites(ctx context.Context, c appteam.Caller) ([]appteam.InviteView, error)
	RevokeInvite(ctx context.Context, c appteam.Caller, inviteID uuid.UUID) error
	Join(ctx context.Context, in appteam.JoinInput) (*appteam.JoinResult, error)
}

// Handler serves /api/v1/team/*.
type Handler struct {
	svc Service
}

// New creates a Handler. A nil svc makes every route answer 503.
func New(svc Service) *Handler { return &Handler{svc: svc} }

// RegisterRoutes mounts the team routes; the caller applies auth first.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/team")
	g.GET("/members", h.ListMembers)
	g.PATCH("/members/:account_id", h.UpdateMember)
	g.DELETE("/members/:account_id", h.RemoveMember)
	g.POST("/invites", h.CreateInvite)
	g.GET("/invites", h.ListInvites)
	g.DELETE("/invites/:id", h.RevokeInvite)
	g.POST("/join", h.Join)
}

// RegisterNotConfigured mounts the same routes answering 501, used when no handler is wired.
func RegisterNotConfigured(rg *gin.RouterGroup, fn gin.HandlerFunc) {
	g := rg.Group("/team")
	g.GET("/members", fn)
	g.PATCH("/members/:account_id", fn)
	g.DELETE("/members/:account_id", fn)
	g.POST("/invites", fn)
	g.GET("/invites", fn)
	g.DELETE("/invites/:id", fn)
	g.POST("/join", fn)
}

func (h *Handler) caller(c *gin.Context) (appteam.Caller, bool) {
	if h.svc == nil {
		httperr.Write(c, errUnavailable())
		return appteam.Caller{}, false
	}
	sub := middleware.GetIDPSubject(c)
	if sub == "" {
		httperr.Write(c, httperr.Unauthorized("unauthorized", "请先登录"))
		return appteam.Caller{}, false
	}
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		httperr.Write(c, httperr.Forbidden("no_shop", "你还没有开通店铺，暂时不能管理团队"))
		return appteam.Caller{}, false
	}
	return appteam.Caller{TenantID: tenantID, Sub: sub, Email: middleware.GetEmail(c), Name: middleware.GetDisplayName(c)}, true
}

func errUnavailable() *httperr.Error {
	return httperr.New(http.StatusServiceUnavailable, "team_unavailable",
		"团队功能暂时用不了，请稍后再试或联系客服", "请稍后再试，一直不行请联系客服")
}

// writeErr maps use-case errors to the Chinese error envelope.
func writeErr(c *gin.Context, err error) {
	if planlimit.Write(c, err) {
		return
	}
	switch {
	case errors.Is(err, appteam.ErrUnavailable):
		httperr.Write(c, errUnavailable())
	case errors.Is(err, appteam.ErrForbidden):
		httperr.Write(c, httperr.Forbidden("forbidden", "只有老板或店长可以做这个操作，店长不能改动老板和其他店长"))
	case errors.Is(err, appteam.ErrNotMember):
		httperr.Write(c, httperr.NotFound("not_member", "没有找到这位成员，可能已经被移出店铺"))
	case errors.Is(err, appteam.ErrAlreadyMemberHere):
		httperr.Write(c, httperr.New(http.StatusConflict, "already_member_here",
			"这是你自己店铺的邀请链接，你已经在店里了。请把链接发给对方，让他用自己的手机和账号打开。",
			"把链接发给对方，让他用自己的账号打开"))
	case errors.Is(err, appteam.ErrAlreadyInShop):
		var as *appteam.AlreadyInShopError
		errors.As(err, &as)
		canAbandon := as != nil && as.CanAbandon
		message := "你已经是一个店铺的成员，暂时一个账号只能属于一个店铺。如果这个邀请就是你所在的店铺，直接登录使用即可；否则请换一个账号来加入，或联系客服。"
		action := "直接登录使用，或换一个账号来加入，或联系客服"
		switch {
		case canAbandon:
			message = "你的店里只有示例数据，可以确认放弃这个空店后再加入：放弃后原店铺会停用、不能恢复，之后你看到的是老板店里的数据。"
			action = "确认放弃空店并加入，或换一个没开过店的账号来加入"
		case as != nil && as.IsOwner:
			message = "你的账号已经开过自己的店，店里已有真实的商品、单据或其他成员，不能放弃。请换一个没开过店的账号来加入，或联系客服。"
			action = "请换一个没开过店的账号来加入，或联系客服"
		}
		// can_abandon_empty_shop tells the join page to offer "give up the empty shop and join".
		c.JSON(http.StatusConflict, gin.H{
			"error":                  "already_in_shop",
			"message":                message,
			"action":                 action,
			"can_abandon_empty_shop": canAbandon,
		})
	case errors.Is(err, appteam.ErrShopNotEmpty):
		httperr.Write(c, httperr.New(http.StatusConflict, "shop_not_empty",
			"你的店铺已经有真实的商品、单据或其他成员，所以不能放弃它。请换一个没开过店的账号来加入，或联系客服。",
			"请换一个没开过店的账号来加入，或联系客服"))
	case errors.Is(err, appteam.ErrInviteInvalid):
		httperr.Write(c, httperr.New(http.StatusGone, "invite_invalid",
			"这个邀请已失效（过期、已被使用或已被撤销）", "请联系店主重新发一个邀请"))
	case errors.Is(err, appteam.ErrLastOwner):
		httperr.Write(c, httperr.Conflict("last_owner", "老板不能被移出店铺，也不能自己退出"))
	case errors.Is(err, appteam.ErrInvalidRole):
		httperr.Write(c, httperr.BadRequest("invalid_role", "请选择列表里的角色", ""))
	case errors.Is(err, appteam.ErrRoleNotForShop):
		httperr.Write(c, httperr.New(http.StatusUnprocessableEntity, "role_not_for_shop",
			"收银只在零售店的收银台使用，请选店员", "请改选店员"))
	case errors.Is(err, appteam.ErrOrgNotReady):
		httperr.Write(c, httperr.Conflict("team_not_ready", "这个店铺的团队功能还没开通，请让老板先打开一次团队页面"))
	default:
		httperr.Write(c, httperr.Wrap(http.StatusInternalServerError, "internal_error",
			"团队信息处理失败，请稍后再试", "", err))
	}
}

func accountIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("account_id"), 10, 64)
	if err != nil || id <= 0 {
		httperr.Write(c, httperr.BadRequest("bad_request", "成员编号不对，请刷新页面后重试", ""))
		return 0, false
	}
	return id, true
}

// ListMembers handles GET /api/v1/team/members.
func (h *Handler) ListMembers(c *gin.Context) {
	cl, ok := h.caller(c)
	if !ok {
		return
	}
	items, err := h.svc.ListMembers(c.Request.Context(), cl)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "cashier_allowed": h.svc.CashierAllowed(c.Request.Context(), cl.TenantID)})
}

type updateMemberRequest struct {
	Role string `json:"role"`
}

// UpdateMember handles PATCH /api/v1/team/members/:account_id.
func (h *Handler) UpdateMember(c *gin.Context) {
	cl, ok := h.caller(c)
	if !ok {
		return
	}
	id, ok := accountIDParam(c)
	if !ok {
		return
	}
	var req updateMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Role == "" {
		httperr.Write(c, httperr.BadRequest("bad_request", "请选择要设置的角色", ""))
		return
	}
	if err := h.svc.UpdateMemberRole(c.Request.Context(), cl, id, domain.Role(req.Role)); err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"account_id": id, "role": req.Role, "role_label": domain.Role(req.Role).Label()})
}

// RemoveMember handles DELETE /api/v1/team/members/:account_id.
func (h *Handler) RemoveMember(c *gin.Context) {
	cl, ok := h.caller(c)
	if !ok {
		return
	}
	id, ok := accountIDParam(c)
	if !ok {
		return
	}
	if err := h.svc.RemoveMember(c.Request.Context(), cl, id); err != nil {
		writeErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type createInviteRequest struct {
	Role     string `json:"role"`
	TTLHours int    `json:"ttl_hours"`
}

// joinPath is the front-end route a shared invite link points at.
const joinPath = "/join?token="

// inviteLink builds the join link. shop and role are display-only hints for the
// join page; what the person actually gets is decided by the invite itself.
func inviteLink(token, inviteID, shopName, roleLabel string) string {
	link := joinPath + token
	if inviteID != "" {
		link += "&i=" + url.QueryEscape(inviteID)
	}
	if shopName != "" {
		link += "&shop=" + url.QueryEscape(shopName)
	}
	return link + "&role=" + url.QueryEscape(roleLabel)
}

// CreateInvite handles POST /api/v1/team/invites. The link is returned once.
func (h *Handler) CreateInvite(c *gin.Context) {
	cl, ok := h.caller(c)
	if !ok {
		return
	}
	var req createInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Role == "" {
		httperr.Write(c, httperr.BadRequest("bad_request", "请选择邀请的角色", ""))
		return
	}
	if req.TTLHours < 0 || req.TTLHours > 168 {
		httperr.Write(c, httperr.BadRequest("bad_request", "邀请有效期最长 7 天", ""))
		return
	}
	res, err := h.svc.CreateInvite(c.Request.Context(), cl, domain.Role(req.Role), req.TTLHours)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"invite_id":  res.InviteID,
		"link":       inviteLink(res.Token, res.InviteID.String(), res.ShopName, domain.Role(req.Role).Label()),
		"role":       req.Role,
		"role_label": domain.Role(req.Role).Label(),
		"expires_at": res.ExpiresAt,
	})
}

// ListInvites handles GET /api/v1/team/invites.
func (h *Handler) ListInvites(c *gin.Context) {
	cl, ok := h.caller(c)
	if !ok {
		return
	}
	items, err := h.svc.ListInvites(c.Request.Context(), cl)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// RevokeInvite handles DELETE /api/v1/team/invites/:id.
func (h *Handler) RevokeInvite(c *gin.Context) {
	cl, ok := h.caller(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httperr.Write(c, httperr.BadRequest("bad_request", "邀请编号不对，请刷新页面后重试", ""))
		return
	}
	if err := h.svc.RevokeInvite(c.Request.Context(), cl, id); err != nil {
		writeErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type joinRequest struct {
	Token string `json:"token"`
	// InviteID is the "i" parameter of the link; it lets the server spot an invite of the caller's own shop.
	InviteID string `json:"invite_id"`
	// AbandonEmptyShop is the explicit confirmation to give up an empty shop of
	// the caller's own (sample data only, sole member) before joining.
	AbandonEmptyShop bool `json:"abandon_empty_shop"`
}

// Join handles POST /api/v1/team/join for any signed-in person.
func (h *Handler) Join(c *gin.Context) {
	if h.svc == nil {
		httperr.Write(c, errUnavailable())
		return
	}
	sub := middleware.GetIDPSubject(c)
	if sub == "" {
		httperr.Write(c, httperr.Unauthorized("unauthorized", "请先登录"))
		return
	}
	var req joinRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Token == "" {
		httperr.Write(c, httperr.BadRequest("bad_request", "邀请链接不完整，请向店主要一个新的邀请", ""))
		return
	}
	// A malformed id is ignored: it is only a hint, the token decides.
	inviteID, _ := uuid.Parse(req.InviteID)
	res, err := h.svc.Join(c.Request.Context(), appteam.JoinInput{
		Sub: sub, Email: middleware.GetEmail(c), Name: middleware.GetDisplayName(c), Token: req.Token,
		InviteID:         inviteID,
		AbandonEmptyShop: req.AbandonEmptyShop,
	})
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"tenant_id":  res.TenantID,
		"shop_name":  res.ShopName,
		"role":       res.Role,
		"role_label": res.Role.Label(),
	})
}
