package entitlement

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// 套餐限额键。平台权益 map 里以这些键（或旧键）下发数值。
const (
	KeyMaxUsers      = "max_users"
	KeyMaxSKUs       = "max_skus"
	KeyMaxWarehouses = "max_warehouses"
)

// Unlimited 表示该项不限。
const Unlimited int64 = -1

// limitCacheTTL 是限额在进程内的缓存时长。
const limitCacheTTL = 60 * time.Second

// limitKeys 是所有受管的限额键，及其兼容的旧键。
var limitKeys = map[string][]string{
	KeyMaxUsers:      {KeyMaxUsers, "users"},
	KeyMaxSKUs:       {KeyMaxSKUs, "skus"},
	KeyMaxWarehouses: {KeyMaxWarehouses, "warehouses"},
}

type limitEntry struct {
	vals map[string]int64
	at   time.Time
}

// WithClock 替换缓存使用的时钟（测试用）。
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Limit 返回租户套餐对 key（max_users / max_skus / max_warehouses）的上限。
// -1 表示不限：键缺失、值无法解析、租户尚未绑定平台账号，以及解析/平台出错
// （fail-open，只打 Warn，且不缓存失败结果）都返回 -1。结果按租户缓存 60 秒。
func (s *Service) Limit(ctx context.Context, tenantID uuid.UUID, key string) (int64, error) {
	if tenantID == uuid.Nil {
		return Unlimited, ErrUnauthenticated
	}
	if _, ok := limitKeys[key]; !ok {
		return Unlimited, nil
	}
	if v, ok := s.limits.Load(tenantID); ok {
		if e := v.(limitEntry); s.now().Sub(e.at) < limitCacheTTL {
			return e.vals[key], nil
		}
	}
	accountID, ok, err := s.tenants.GetPlatformAccountID(ctx, tenantID)
	if err != nil {
		s.log.Warn("entitlement: limit resolve failed — treating as unlimited",
			"key", key, "tenant", tenantID.String(), "err", err)
		return Unlimited, nil
	}
	vals := map[string]int64{KeyMaxUsers: Unlimited, KeyMaxSKUs: Unlimited, KeyMaxWarehouses: Unlimited}
	if ok {
		ents, err := s.platform.GetEntitlements(ctx, accountID, productID)
		if err != nil {
			s.log.Warn("entitlement: limit fetch failed — treating as unlimited",
				"key", key, "account_id", accountID, "err", err)
			return Unlimited, nil
		}
		for k, aliases := range limitKeys {
			vals[k] = parseLimit(ents, aliases)
		}
	}
	s.limits.Store(tenantID, limitEntry{vals: vals, at: s.now()})
	return vals[key], nil
}

// parseLimit 依次尝试各别名；缺失、非数字、负数都按不限处理。
func parseLimit(ents map[string]string, aliases []string) int64 {
	for _, a := range aliases {
		raw, present := ents[a]
		if !present {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || n < 0 {
			return Unlimited
		}
		return n
	}
	return Unlimited
}
