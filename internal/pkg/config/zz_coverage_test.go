// zz_coverage_test.go raises internal/pkg/config statement coverage toward
// the project floor (repo ≥80%). Reuses setEnv + fullEnv from config_test.go
// (same config_test package) — no new env-handling helpers introduced.
package config_test

import (
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
)

// TestNormalizeIssuer covers the pure function directly: empty passthrough,
// bare-host → https:// prefix, and both already-schemed forms left unchanged.
func TestNormalizeIssuer(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"empty stays empty", "", ""},
		{"bare host gets https prefix", "identity.lurus.cn", "https://identity.lurus.cn"},
		{"http scheme left unchanged", "http://identity.lurus.cn", "http://identity.lurus.cn"},
		{"https scheme left unchanged", "https://identity.lurus.cn", "https://identity.lurus.cn"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := config.NormalizeIssuer(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeIssuer(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestConfig_AllOptionalDefaults locks every optional() default in one place,
// hand-derived from config.go's Load body (not read back from a prior run).
func TestConfig_AllOptionalDefaults(t *testing.T) {
	env := fullEnv()
	// Explicitly clear every optional var so we exercise the default branch of
	// optional(), independent of whatever happens to be set in the local shell.
	for _, k := range []string{
		"PORT", "LOG_LEVEL", "GIN_MODE", "SERVICE_VERSION", "SHUTDOWN_TIMEOUT",
		"MIGRATE_ON_BOOT", "PLATFORM_BASE_URL", "PLATFORM_INTERNAL_KEY",
		"PLATFORM_NOTIFY_URL", "OIDC_JWKS_PATH", "NEWAPI_BASE_URL", "NEWAPI_API_KEY",
		"DEFAULT_AI_MODEL", "AI_PLAN_TTL_SECONDS", "MEMORUS_BASE_URL", "MEMORUS_API_KEY",
		"SHOPIFY_WEBHOOK_SECRET",
	} {
		env[k] = ""
	}
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	wantDefaults := map[string]string{
		"Port":                 cfg.Port,
		"LogLevel":             cfg.LogLevel,
		"GinMode":              cfg.GinMode,
		"ServiceVersion":       cfg.ServiceVersion,
		"ShutdownTimeout":      cfg.ShutdownTimeout.String(),
		"PlatformBaseURL":      cfg.PlatformBaseURL,
		"PlatformNotifyURL":    cfg.PlatformNotifyURL,
		"OIDCJWKSPath":         cfg.OIDCJWKSPath,
		"NewAPIBaseURL":        cfg.NewAPIBaseURL,
		"DefaultAIModel":       cfg.DefaultAIModel,
		"MemoryBaseURL":        cfg.MemoryBaseURL,
		"NewAPIKey":            cfg.NewAPIKey,
		"MemoryAPIKey":         cfg.MemoryAPIKey,
		"PlatformInternalKey":  cfg.PlatformInternalKey,
		"ShopifyWebhookSecret": cfg.ShopifyWebhookSecret,
	}
	expect := map[string]string{
		"Port":                 "18200",
		"LogLevel":             "info",
		"GinMode":              "release",
		"ServiceVersion":       "dev",
		"ShutdownTimeout":      "5s",
		"PlatformBaseURL":      "http://platform-core.lurus-platform.svc:18104",
		"PlatformNotifyURL":    "http://notification.lurus-platform.svc:18900",
		"OIDCJWKSPath":         "/oauth/v2/keys",
		"NewAPIBaseURL":        "https://newapi.lurus.cn/v1",
		"DefaultAIModel":       "deepseek-v4-flash",
		"MemoryBaseURL":        "http://memorus.lurus-system.svc:8880",
		"NewAPIKey":            "",
		"MemoryAPIKey":         "",
		"PlatformInternalKey":  "",
		"ShopifyWebhookSecret": "",
	}
	for field, want := range expect {
		if got := wantDefaults[field]; got != want {
			t.Errorf("default %s: want %q, got %q", field, want, got)
		}
	}
	if cfg.AIPlanTTLSeconds != 1800 {
		t.Errorf("default AIPlanTTLSeconds: want 1800, got %d", cfg.AIPlanTTLSeconds)
	}
	if !cfg.MigrateOnBoot {
		t.Error("default MigrateOnBoot: want true, got false")
	}
}

// TestConfig_OptionalOverrides confirms optional() honours explicit env
// values instead of always falling through to the default.
func TestConfig_OptionalOverrides(t *testing.T) {
	env := fullEnv()
	env["PORT"] = "9999"
	env["LOG_LEVEL"] = "debug"
	env["GIN_MODE"] = "debug"
	env["SERVICE_VERSION"] = "v1.2.3"
	env["SHUTDOWN_TIMEOUT"] = "30s"
	env["PLATFORM_NOTIFY_URL"] = "http://notify.test.local:1"
	env["OIDC_JWKS_PATH"] = "/custom/jwks"
	env["NEWAPI_BASE_URL"] = "https://custom.newapi.local/v1"
	env["NEWAPI_API_KEY"] = "newapi-secret"
	env["DEFAULT_AI_MODEL"] = "custom-model"
	env["MEMORUS_BASE_URL"] = "http://memory.test.local:1"
	env["MEMORUS_API_KEY"] = "memory-secret"
	env["SHOPIFY_WEBHOOK_SECRET"] = "shopify-secret"
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cases := map[string]struct{ got, want string }{
		"Port":                 {cfg.Port, "9999"},
		"LogLevel":             {cfg.LogLevel, "debug"},
		"GinMode":              {cfg.GinMode, "debug"},
		"ServiceVersion":       {cfg.ServiceVersion, "v1.2.3"},
		"ShutdownTimeout":      {cfg.ShutdownTimeout.String(), "30s"},
		"PlatformNotifyURL":    {cfg.PlatformNotifyURL, "http://notify.test.local:1"},
		"OIDCJWKSPath":         {cfg.OIDCJWKSPath, "/custom/jwks"},
		"NewAPIBaseURL":        {cfg.NewAPIBaseURL, "https://custom.newapi.local/v1"},
		"NewAPIKey":            {cfg.NewAPIKey, "newapi-secret"},
		"DefaultAIModel":       {cfg.DefaultAIModel, "custom-model"},
		"MemoryBaseURL":        {cfg.MemoryBaseURL, "http://memory.test.local:1"},
		"MemoryAPIKey":         {cfg.MemoryAPIKey, "memory-secret"},
		"ShopifyWebhookSecret": {cfg.ShopifyWebhookSecret, "shopify-secret"},
	}
	for field, c := range cases {
		if c.got != c.want {
			t.Errorf("override %s: want %q, got %q", field, c.want, c.got)
		}
	}
}

// TestConfig_AIPlanTTLSeconds_ValidInt covers the Sscanf-parsed branch with a
// well-formed integer, distinct from the unset-default case already covered
// by TestConfig_AllOptionalDefaults.
func TestConfig_AIPlanTTLSeconds_ValidInt(t *testing.T) {
	env := fullEnv()
	env["AI_PLAN_TTL_SECONDS"] = "3600"
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AIPlanTTLSeconds != 3600 {
		t.Errorf("AIPlanTTLSeconds: want 3600, got %d", cfg.AIPlanTTLSeconds)
	}
}

// TestConfig_AIPlanTTLSeconds_NonNumeric_LeavesDefault covers the
// errcheck-ignored Sscanf failure path: a non-numeric value fails to parse,
// and since aiPlanTTL was seeded to 1800 before the Sscanf call, a parse
// failure leaves it at that seed value (Sscanf does not zero the destination
// on error — it just doesn't write to it).
func TestConfig_AIPlanTTLSeconds_NonNumeric_LeavesDefault(t *testing.T) {
	env := fullEnv()
	env["AI_PLAN_TTL_SECONDS"] = "not-a-number"
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AIPlanTTLSeconds != 1800 {
		t.Errorf("AIPlanTTLSeconds on non-numeric input: want 1800 (seed unchanged), got %d", cfg.AIPlanTTLSeconds)
	}
}

// TestConfig_MigrateOnBoot_TrueOnGarbageValue covers the `!= "false"` logic:
// any value other than the literal string "false" (including nonsense) must
// leave migrations enabled — the switch is opt-out, not opt-in.
func TestConfig_MigrateOnBoot_TrueOnGarbageValue(t *testing.T) {
	env := fullEnv()
	env["MIGRATE_ON_BOOT"] = "definitely-not-a-bool"
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.MigrateOnBoot {
		t.Error("MigrateOnBoot: want true for any value != \"false\", got false")
	}
}

// TestConfig_MissingDatabaseDSN_ErrorMentionsVar locks the required() error
// message contract: it must name the missing var so operators know exactly
// what to fix (per package doc comment).
func TestConfig_MissingDatabaseDSN_ErrorMentionsVar(t *testing.T) {
	env := fullEnv()
	env["DATABASE_DSN"] = ""
	setEnv(t, env)

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when DATABASE_DSN is missing, got nil")
	}
	if !strings.Contains(err.Error(), "DATABASE_DSN") {
		t.Errorf("error must name DATABASE_DSN, got: %v", err)
	}
}

// TestConfig_MissingRedisURL_ErrorMentionsVar mirrors the DATABASE_DSN check
// for REDIS_URL — required() is shared code but each call site's message
// must surface the right variable name.
func TestConfig_MissingRedisURL_ErrorMentionsVar(t *testing.T) {
	env := fullEnv()
	env["REDIS_URL"] = ""
	setEnv(t, env)

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when REDIS_URL is missing, got nil")
	}
	if !strings.Contains(err.Error(), "REDIS_URL") {
		t.Errorf("error must name REDIS_URL, got: %v", err)
	}
}

// TestConfig_MissingNATSURL_ErrorMentionsVar mirrors the DATABASE_DSN check
// for NATS_URL.
func TestConfig_MissingNATSURL_ErrorMentionsVar(t *testing.T) {
	env := fullEnv()
	env["NATS_URL"] = ""
	setEnv(t, env)

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when NATS_URL is missing, got nil")
	}
	if !strings.Contains(err.Error(), "NATS_URL") {
		t.Errorf("error must name NATS_URL, got: %v", err)
	}
}

// TestConfig_OIDCAudienceRequired_ErrorMentionsVar locks the cross-app
// isolation gate's error message: OIDC_ISSUER set + OIDC_AUDIENCE empty must
// name OIDC_AUDIENCE so operators fix the right var.
func TestConfig_OIDCAudienceRequired_ErrorMentionsVar(t *testing.T) {
	env := fullEnv()
	env["OIDC_ISSUER"] = "identity.lurus.cn"
	env["OIDC_AUDIENCE"] = ""
	setEnv(t, env)

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when OIDC_ISSUER set but OIDC_AUDIENCE missing, got nil")
	}
	if !strings.Contains(err.Error(), "OIDC_AUDIENCE") {
		t.Errorf("error must name OIDC_AUDIENCE, got: %v", err)
	}
}

// TestConfig_AuthBoundaryFastFail_ErrorMentionsIssuer locks the multi-tenant
// security gate's error message: OIDC_ISSUER empty + TALLY_DEV_MODE!=true
// must name OIDC_ISSUER (the business invariant under test — refuses to boot
// the entire /api/v1 surface UNAUTHENTICATED).
func TestConfig_AuthBoundaryFastFail_ErrorMentionsIssuer(t *testing.T) {
	env := fullEnv()
	env["OIDC_ISSUER"] = ""
	env["TALLY_DEV_MODE"] = "false"
	setEnv(t, env)

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when OIDC_ISSUER empty and TALLY_DEV_MODE!=true, got nil")
	}
	if !strings.Contains(err.Error(), "OIDC_ISSUER") {
		t.Errorf("error must name OIDC_ISSUER, got: %v", err)
	}
}

// TestConfig_PlatformBaseURL_ExplicitOverride complements the
// platform_honesty_test.go defaults check by exercising the override branch
// of optional() for PLATFORM_BASE_URL specifically (distinct code path from
// the SHOPIFY/NEWAPI overrides already covered above, since this field feeds
// the billing client per config.go's doc comment).
func TestConfig_PlatformBaseURL_ExplicitOverride(t *testing.T) {
	env := fullEnv()
	env["PLATFORM_BASE_URL"] = "http://platform-override.test.local:18104"
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PlatformBaseURL != "http://platform-override.test.local:18104" {
		t.Errorf("PlatformBaseURL override: got %q", cfg.PlatformBaseURL)
	}
}
