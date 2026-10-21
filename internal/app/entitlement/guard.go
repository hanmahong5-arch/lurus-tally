package entitlement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Mode 是套餐限额的执行模式。
type Mode string

const (
	ModeOff     Mode = "off"     // 不检查
	ModeShadow  Mode = "shadow"  // 只记日志收集用量，不拦截
	ModeEnforce Mode = "enforce" // 超限即拒绝
)

// ParseMode 解析配置值；空或未知值按 shadow（种子期默认）处理。
func ParseMode(v string) Mode {
	switch Mode(v) {
	case ModeOff, ModeEnforce:
		return Mode(v)
	default:
		return ModeShadow
	}
}

// UsageCounter 统计租户当前用量。
type UsageCounter interface {
	CountUsers(ctx context.Context, tenantID uuid.UUID) (int64, error)
	CountSKUs(ctx context.Context, tenantID uuid.UUID) (int64, error)
	CountWarehouses(ctx context.Context, tenantID uuid.UUID) (int64, error)
}

// ErrPlanLimitReached 表示再增加 delta 个对象会超过套餐上限（仅 enforce 模式返回）。
type ErrPlanLimitReached struct {
	Key   string
	Used  int64
	Limit int64
}

func (e *ErrPlanLimitReached) Error() string {
	return fmt.Sprintf("entitlement: plan limit reached key=%s used=%d limit=%d", e.Key, e.Used, e.Limit)
}

// Label 是限额对象的中文名。
func (e *ErrPlanLimitReached) Label() string { return KeyLabel(e.Key) }

// Message 是给店主看的中文提示。
func (e *ErrPlanLimitReached) Message() string {
	return fmt.Sprintf("当前套餐最多 %d 个%s，已用 %d，请升级套餐", e.Limit, e.Label(), e.Used)
}

// KeyLabel 返回限额键对应的中文对象名。
func KeyLabel(key string) string {
	switch key {
	case KeyMaxUsers:
		return "成员"
	case KeyMaxSKUs:
		return "商品"
	case KeyMaxWarehouses:
		return "仓库"
	}
	return key
}

// LimitStatus 是某一项限额的当前状态（供订阅状态接口展示）。
type LimitStatus struct {
	Limit    int64 `json:"limit"`
	Used     int64 `json:"used"`
	Enforced bool  `json:"enforced"`
}

// Guard 在创建成员/商品/仓库前检查套餐上限。所有方法对 nil 接收者安全（视为 off）。
type Guard struct {
	svc   *Service
	mode  Mode
	usage UsageCounter
	log   *slog.Logger

	// statusCache 让订阅状态接口（横幅会轮询）不必每次都跑三条 COUNT。
	statusCache sync.Map // uuid.UUID -> statusEntry
	now         func() time.Time
}

// statusTTL 是 Status 结果的缓存时长，仅影响用量展示，不影响 Check。
const statusTTL = 10 * time.Second

type statusEntry struct {
	st map[string]LimitStatus
	at time.Time
}

// WithClock 替换 Status 缓存使用的时钟（测试用）。
func (g *Guard) WithClock(now func() time.Time) *Guard {
	g.now = now
	return g
}

// NewGuard 构造 Guard。log 为 nil 时用默认 logger。
func NewGuard(svc *Service, mode Mode, usage UsageCounter, log *slog.Logger) *Guard {
	if log == nil {
		log = slog.Default()
	}
	return &Guard{svc: svc, mode: mode, usage: usage, log: log, now: time.Now}
}

// Mode 返回当前执行模式（nil Guard 为 off）。
func (g *Guard) Mode() Mode {
	if g == nil {
		return ModeOff
	}
	return g.mode
}

func (g *Guard) used(ctx context.Context, tenantID uuid.UUID, key string) (int64, error) {
	switch key {
	case KeyMaxUsers:
		return g.usage.CountUsers(ctx, tenantID)
	case KeyMaxSKUs:
		return g.usage.CountSKUs(ctx, tenantID)
	case KeyMaxWarehouses:
		return g.usage.CountWarehouses(ctx, tenantID)
	}
	return 0, fmt.Errorf("entitlement: unknown limit key %q", key)
}

// Check 检查再新增 delta 个 key 对象是否超限。off 直接放行；上限不限放行；
// 查额度或用量出错 fail-open（只记 Warn）；超限时 shadow 只记 Info 日志并放行，
// enforce 返回 *ErrPlanLimitReached。
func (g *Guard) Check(ctx context.Context, tenantID uuid.UUID, key string, delta int) error {
	if g == nil || g.svc == nil || g.usage == nil || g.mode == ModeOff || delta <= 0 {
		return nil
	}
	limit, err := g.svc.Limit(ctx, tenantID, key)
	if err != nil || limit < 0 {
		return nil
	}
	used, err := g.used(ctx, tenantID, key)
	if err != nil {
		g.log.Warn("entitlement: usage count failed — skipping plan limit check",
			"key", key, "tenant", tenantID.String(), "err", err)
		return nil
	}
	if used+int64(delta) <= limit {
		return nil
	}
	if g.mode == ModeEnforce {
		return &ErrPlanLimitReached{Key: key, Used: used, Limit: limit}
	}
	g.log.Info("plan_limit_shadow",
		"key", key, "used", used, "limit", limit, "delta", delta, "tenant", tenantID.String())
	return nil
}

// Status 返回三项限额的上限与用量；取不到的项上限按不限、用量 0。
func (g *Guard) Status(ctx context.Context, tenantID uuid.UUID) map[string]LimitStatus {
	if g == nil || g.svc == nil || g.usage == nil {
		return nil
	}
	if v, ok := g.statusCache.Load(tenantID); ok {
		if e := v.(statusEntry); g.now().Sub(e.at) < statusTTL {
			return e.st
		}
	}
	out := make(map[string]LimitStatus, 3)
	for _, key := range []string{KeyMaxUsers, KeyMaxSKUs, KeyMaxWarehouses} {
		st := LimitStatus{Limit: Unlimited, Enforced: g.mode == ModeEnforce}
		if l, err := g.svc.Limit(ctx, tenantID, key); err == nil {
			st.Limit = l
		}
		if u, err := g.used(ctx, tenantID, key); err == nil {
			st.Used = u
		} else {
			g.log.Warn("entitlement: usage count failed", "key", key, "err", err)
		}
		out[key] = st
	}
	g.statusCache.Store(tenantID, statusEntry{st: out, at: g.now()})
	return out
}

// IsPlanLimit 判断 err 是否为套餐超限错误并取出。
func IsPlanLimit(err error) (*ErrPlanLimitReached, bool) {
	var e *ErrPlanLimitReached
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
