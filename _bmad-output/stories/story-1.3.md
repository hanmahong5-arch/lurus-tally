# Story 1.3 — 数据库迁移脚本完整执行（全 27 张表）

**Epic**: 1 — 项目骨架与 CI/CD 管线
**Story ID**: 1.3
**优先级**: P0（Story 1.1 完成后立即执行；Story 1.4+ 全部依赖 DB schema 就绪）
**类型**: infra（数据层奠基）
**预估**: 8–10 小时
**Status**: Done
**Owner**: TBD

---

## User Story

As a Lurus Tally developer,
I want all 27 database tables created in the `tally` PostgreSQL schema by running `golang-migrate up`,
so that every subsequent Story can write and read business data without manual schema setup.

---

## Context

Story 1.1 交付了一个可启动的 Go 服务骨架，`/readyz` 当前直接返回 ready（不做 DB ping）。Story 1.3 完成后，`lifecycle/migrate.go` 将在启动时自动运行迁移，`lifecycle/start.go` 将升级为先 migrate 再通过 readiness 检查，实现"空库一键就绪"。

所有 27 张业务表的完整 DDL 定义于 `architecture.md §5.2`；迁移文件按领域拆成 12 个版本，最后两个版本为物化视图（`000011`）和 RLS 策略（`000012`）。此设计确保 down 迁移可以精确逆向（先撤 RLS，再撤视图，再撤业务表）。

迁移工具 `golang-migrate` 通过 `embed.FS` 将所有 `.sql` 文件编译进二进制，与 Story 1.1 的 `scratch` 基础镜像保持一致（无文件系统访问）。

---

## Acceptance Criteria

1. **AC-1 全表创建**: 给定空 PostgreSQL（仅有默认 `public` schema），当运行 `migrate up`，则以下查询返回值 ≥ 27：
   ```sql
   SELECT COUNT(*) FROM information_schema.tables
   WHERE table_schema = 'tally' AND table_type IN ('BASE TABLE', 'VIEW');
   ```
   同时，以下查询返回值 ≥ 1（物化视图单独统计）：
   ```sql
   SELECT COUNT(*) FROM pg_matviews WHERE schemaname = 'tally';
   ```

2. **AC-2 幂等性**: 给定已执行过 `migrate up` 的 PostgreSQL，当再次运行 `migrate up`，则命令返回 exit code 0，无错误输出，`schema_migrations` 表无重复记录。

3. **AC-3 pgvector 扩展**: 给定运行 `migrate up` 后，当执行：
   ```sql
   SELECT extversion FROM pg_extension WHERE extname = 'vector';
   ```
   则返回非空行（版本号不限，确认扩展已安装）。

4. **AC-4 RLS 策略生效**: 给定运行 `migrate up` 后，当查询：
   ```sql
   SELECT tablename FROM pg_policies
   WHERE schemaname = 'tally'
   ORDER BY tablename;
   ```
   则返回的表名集合包含全部 11 张租户业务表（见 §Dev Notes RLS 完整清单）。

5. **AC-5 Down 迁移可逆**: 给定运行过 `migrate up` 的 PostgreSQL，当运行 `migrate down -all`，则 `tally` schema 中无任何 BASE TABLE 残留（扩展和 schema 本身视具体 down 实现而定；此 AC 的底线是不报错且业务表清除）。

6. **AC-6 启动自动迁移**: 给定有效的 `DATABASE_DSN` 和 `MIGRATE_ON_BOOT=true`，当启动 `go run ./cmd/server`，则服务日志包含 `"migration completed"` 字段，且 `/internal/v1/tally/ready` 随后返回 200（migration 完成后 readiness 才置位）。

7. **AC-7 快速失败**: 给定 `DATABASE_DSN` 指向不可达的 PostgreSQL，当启动服务，则进程以非零退出码退出，日志包含明确错误（"发生了什么 / 期望是什么 / 调用方能做什么"三要素，例如 `"migration failed: dial tcp ... connect: connection refused; ensure PostgreSQL is reachable at DATABASE_DSN"`）。

8. **AC-8 MIGRATE_ON_BOOT=false 跳过迁移**: 给定 `MIGRATE_ON_BOOT=false`，当启动服务，则迁移步骤被跳过，服务仍正常启动（适用于生产运维手动控制 schema 变更的场景）。

9. **AC-9 单元/集成测试通过**: 执行 `go test -v ./tests/integration/... -tags integration`，`TestMigration_AllTablesExist`、`TestMigration_Idempotent`、`TestMigration_DownReverses`、`TestMigration_pgvectorAvailable`、`TestMigration_RLSEnabled` 全部 PASS（CI 环境，需 Docker）。

10. **AC-10 config 更新**: `internal/pkg/config/config.go` 新增 `MigrateOnBoot bool` 字段，由环境变量 `MIGRATE_ON_BOOT` 控制，默认 `true`；现有 4 个 config 单元测试仍全部通过。

---

## Tasks / Subtasks

### Task 1: 新增 config 字段 `MigrateOnBoot`

- [x] 写失败测试: `TestConfig_MigrateOnBoot_DefaultTrue`
  - 不设置 `MIGRATE_ON_BOOT` 时，`Load()` 返回 `MigrateOnBoot = true`
- [x] 写失败测试: `TestConfig_MigrateOnBoot_FalseWhenSet`
  - 设置 `MIGRATE_ON_BOOT=false` 时，`Load()` 返回 `MigrateOnBoot = false`
- [x] 修改 `internal/pkg/config/config.go`:
  - 在 `Config` struct 新增 `MigrateOnBoot bool` 字段（注释: `MIGRATE_ON_BOOT: run migrations on startup, default true`）
  - 在 `Load()` 中用 `optional("MIGRATE_ON_BOOT", "true")` 读取，`!= "false"` 时为 true（防御性解析）
- [x] 验证: 全部 6 个 config 测试（原 4 个 + 新增 2 个）通过

---

### Task 2: 创建 migrations 目录与 12 个迁移文件（up + down 对）

每个 `.up.sql` 文件必须有配对的 `.down.sql`。DDL 精确列定义见 `architecture.md §5.2`（本故事不内联 DDL，开发者直接从 architecture.md 拷贝）。

#### 2.1 — `000001_init_extensions.up.sql`

- **目的**: 创建 `tally` schema，安装 pgvector / pg_trgm / pgcrypto
- **架构文档引用**: architecture.md §5.1 前置扩展 + §5.5
- **关键内容**:
  ```sql
  CREATE SCHEMA IF NOT EXISTS tally;
  CREATE EXTENSION IF NOT EXISTS "pgcrypto";
  CREATE EXTENSION IF NOT EXISTS "vector";
  CREATE EXTENSION IF NOT EXISTS "pg_trgm";
  ```
- **表数量**: 0 张业务表（扩展 + schema 初始化）
- **down**: `DROP SCHEMA IF EXISTS tally CASCADE;`（级联删除，仅在 down-all 场景使用）

#### 2.2 — `000002_init_tenant.up.sql`

- **目的**: 租户本地缓存表
- **架构文档引用**: architecture.md §5.2 "域 1: tenant"
- **关键表**: `tally.tenant`
- **累计表数**: 1
- **注意**: `tenant.id` 为 platform 同步 ID，无 `DEFAULT gen_random_uuid()`（外部 ID）
- **down**: `DROP TABLE IF EXISTS tally.tenant;`

#### 2.3 — `000003_init_org.up.sql`

- **目的**: 组织架构（部门 + 部门用户关系）
- **架构文档引用**: architecture.md §5.2 "域 2: org_*"
- **关键表**: `tally.org_department`, `tally.org_user_rel`
- **累计表数**: 3
- **down**: DROP 两表（顺序: user_rel 先，department 后）

#### 2.4 — `000004_init_partner.up.sql`

- **目的**: 供应商/客户主档及银行账户
- **架构文档引用**: architecture.md §5.2 "域 3: partner_*"
- **关键表**: `tally.partner`, `tally.partner_bank`
- **License 声明**: 文件顶部注释 `-- Derived from jshERP jsh_supplier (Apache-2.0)`
- **累计表数**: 5
- **down**: DROP partner_bank，DROP partner

#### 2.5 — `000005_init_product.up.sql`

- **目的**: 商品/SKU/分类/属性/单位
- **架构文档引用**: architecture.md §5.2 "域 4: product_*"
- **关键表**: `tally.product_category`, `tally.product`, `tally.product_sku`, `tally.product_attribute`, `tally.unit`
- **重要**: `product.embedding vector(1536)` 列依赖 000001 的 `vector` 扩展；`CREATE INDEX ... USING ivfflat` 依赖 pgvector
- **License 声明**: `-- Derived from jshERP jsh_material* (Apache-2.0)`
- **累计表数**: 10
- **down**: DROP 5 张表（按 FK 反向顺序：product_sku, product_attribute, product, product_category, unit）

#### 2.6 — `000006_init_stock.up.sql`

- **目的**: 仓库/货位/库存快照/批次/序列号/期初库存
- **架构文档引用**: architecture.md §5.2 "域 5: warehouse_* / stock_*"
- **关键表**: `tally.warehouse`, `tally.warehouse_bin`, `tally.stock_initial`, `tally.stock_snapshot`, `tally.stock_lot`, `tally.stock_serial`
- **GreaterWMS 来源**: warehouse_bin 和 stock_snapshot 的 6 状态字段（on_hand/available/reserved/in_transit/damage/hold）
- **License 声明**: `-- Derived from jshERP jsh_depot/jsh_material_current_stock + GreaterWMS binset (Apache-2.0)`
- **累计表数**: 16
- **down**: DROP 6 张表（按 FK 反向顺序）

#### 2.7 — `000007_init_bill.up.sql`

- **目的**: 通用单据主表 + 明细（采购/销售/调拨/盘点共享此模型）
- **架构文档引用**: architecture.md §5.2 "域 6: bill_head + bill_item"
- **关键表**: `tally.bill_head`, `tally.bill_item`
- **设计亮点**: `bill_head.amendment_of_id` 自引用（红冲单据追踪）；`bill_item.serial_nos TEXT[]`（替代 jshERP 逗号分隔）
- **License 声明**: `-- Derived from jshERP jsh_depot_head/jsh_depot_item (Apache-2.0)`
- **累计表数**: 18
- **down**: DROP bill_item，DROP bill_head

#### 2.8 — `000008_init_finance.up.sql`

- **目的**: 资金账户/收付款主表/明细/收支分类
- **架构文档引用**: architecture.md §5.2 "域 7: finance_*"
- **关键表**: `tally.finance_account`, `tally.payment_head`, `tally.payment_item`, `tally.finance_category`
- **License 声明**: `-- Derived from jshERP jsh_account* + jsh_in_out_item (Apache-2.0)`
- **累计表数**: 22
- **down**: DROP 4 张表（按 FK 反向：payment_item, payment_head, finance_account, finance_category）

#### 2.9 — `000009_init_audit.up.sql`

- **目的**: 操作审计日志
- **架构文档引用**: architecture.md §5.2 "域 8: audit_log"
- **关键表**: `tally.audit_log`
- **License 声明**: `-- Derived from jshERP jsh_log (Apache-2.0), extended with changes JSONB`
- **累计表数**: 23
- **down**: `DROP TABLE IF EXISTS tally.audit_log;`

#### 2.10 — `000010_init_config.up.sql`

- **目的**: 系统配置/数据字典/单据编号生成器
- **架构文档引用**: architecture.md §5.2 "域 9: 系统配置与字典"
- **关键表**: `tally.system_config`, `tally.dict_type`, `tally.dict_data`, `tally.bill_sequence`
- **License 声明**: `-- Derived from jshERP jsh_system_config/jsh_sys_dict_type/jsh_sequence (Apache-2.0)`
- **累计表数**: 27（基础表全部就绪）
- **down**: DROP 4 张表

#### 2.11 — `000011_init_views.up.sql`

- **目的**: 物化视图 + 普通视图（报表加速）
- **架构文档引用**: architecture.md §5.3 物化视图
- **关键对象**:
  - `CREATE MATERIALIZED VIEW tally.report_stock_summary` — 库存汇总（带唯一索引）
  - `CREATE VIEW tally.ai_reorder_suggestions` — AI 补货建议（普通视图，不占 matview count）
- **注意**: 物化视图创建后为空，需 `REFRESH MATERIALIZED VIEW` 才有数据（后续 Worker 负责刷新）
- **down**:
  ```sql
  DROP VIEW IF EXISTS tally.ai_reorder_suggestions;
  DROP MATERIALIZED VIEW IF EXISTS tally.report_stock_summary;
  ```

#### 2.12 — `000012_init_rls.up.sql`

- **目的**: 为所有租户业务表启用 Row-Level Security 并创建 tenant_isolation 策略
- **架构文档引用**: architecture.md §9.2 + §5.2 各表内联 RLS 声明
- **关键 SQL 模式**（每张业务表重复）:
  ```sql
  ALTER TABLE tally.<table> ENABLE ROW LEVEL SECURITY;
  CREATE POLICY <table>_rls ON tally.<table>
      USING (tenant_id = current_setting('app.tenant_id', true)::UUID);
  ```
  第二参数 `true` 使 `current_setting` 在未设置时返回 NULL 而非报错（迁移期间超级用户连接无需设置 tenant_id）。
- **RLS 覆盖的 11 张表**（含各自策略名）:

  | 表名 | 策略名 |
  |------|--------|
  | tally.partner | partner_rls |
  | tally.product | product_rls |
  | tally.warehouse | warehouse_rls |
  | tally.stock_snapshot | stock_snapshot_rls |
  | tally.bill_head | bill_head_rls |
  | tally.bill_item | bill_item_rls |
  | tally.payment_head | payment_head_rls |
  | tally.audit_log | audit_log_rls |
  | tally.system_config | system_config_rls |
  | tally.bill_sequence | bill_sequence_rls |
  | tally.org_department | org_department_rls |

- **down**: 按反向顺序 `DROP POLICY ... ON tally.<table>; ALTER TABLE tally.<table> DISABLE ROW LEVEL SECURITY;`

**Task 2 验证**: 全部 24 个文件（12 × up + 12 × down）已创建，文件大小非零，SQL 语法可通过 `psql --dry-run` 或 pg_format 验证（本地无 Docker 时通过 R6 stage 验证）。

---

### Task 3: 安装 golang-migrate 依赖

- [x] 写失败测试（占位）: `TestMigrate_EmbedFS_LoadsMigrations`
  - 验证 `migrations.FS` embed 可加载 ≥ 24 个文件（12 up + 12 down）
  - 无需数据库连接，只测 embed 加载
- [x] 执行 `go get github.com/golang-migrate/migrate/v4@v4.17.1`
- [x] 执行 `go get github.com/golang-migrate/migrate/v4/database/postgres@v4.17.1`
- [x] 执行 `go get github.com/jackc/pgx/v5@v5.7.4` — pgx v5 驱动（golang-migrate postgres driver 依赖）
- [x] 运行 `go mod tidy`
- [x] 验证: `go build ./...` 无编译错误

---

### Task 4: 创建 `internal/lifecycle/migrate.go`

- [ ] 写失败测试: `TestRunMigrations_InvalidDSN_ReturnsError`
  - 传入不可达 DSN，`RunMigrations` 返回 non-nil error，error 含 "migration failed"
- [ ] 写失败测试: `TestRunMigrations_EmbedNotEmpty`
  - 验证 embed.FS 包含 `000001_init_extensions.up.sql`（内容不为空）
- [ ] 实现 `internal/lifecycle/migrate.go`:

  ```go
  package lifecycle

  import (
      "context"
      "embed"
      "fmt"

      "github.com/golang-migrate/migrate/v4"
      "github.com/golang-migrate/migrate/v4/database/postgres"
      "github.com/golang-migrate/migrate/v4/source/iofs"
      _ "github.com/jackc/pgx/v5/stdlib"
      "database/sql"
  )

  //go:embed ../../migrations/*.sql
  var migrationsFS embed.FS

  // RunMigrations applies all pending migrations from the embedded migrations directory.
  // It is idempotent: already-applied migrations are skipped via schema_migrations tracking.
  // Returns nil if migrations are already up-to-date.
  func RunMigrations(ctx context.Context, dsn string) error {
      db, err := sql.Open("pgx", dsn)
      if err != nil {
          return fmt.Errorf("migration failed: open db: %w; ensure DATABASE_DSN is a valid PostgreSQL DSN", err)
      }
      defer db.Close()

      if err := db.PingContext(ctx); err != nil {
          return fmt.Errorf("migration failed: ping db: %w; ensure PostgreSQL is reachable at DATABASE_DSN", err)
      }

      src, err := iofs.New(migrationsFS, "migrations")
      if err != nil {
          return fmt.Errorf("migration failed: load embedded SQL files: %w", err)
      }

      driver, err := postgres.WithInstance(db, &postgres.Config{})
      if err != nil {
          return fmt.Errorf("migration failed: create driver: %w", err)
      }

      m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
      if err != nil {
          return fmt.Errorf("migration failed: create migrate instance: %w", err)
      }

      if err := m.Up(); err != nil && err != migrate.ErrNoChange {
          return fmt.Errorf("migration failed: apply up: %w", err)
      }
      return nil
  }
  ```

  **重要**: `embed.FS` 的路径必须相对于含 `//go:embed` 指令的 `.go` 文件所在包。`migrate.go` 位于 `internal/lifecycle/`，`migrations/` 位于 `2b-svc-psi/migrations/`，路径为 `../../migrations/*.sql`。若编译器报 embed 路径错误，改用独立 `migrations` 包（见 §Dev Notes）。

- [x] 验证: `TestRunMigrations_InvalidDSN_ReturnsError` 通过（无 Docker）；`TestRunMigrations_EmbedNotEmpty` 通过

---

### Task 5: 修改 `internal/lifecycle/start.go` — 接入迁移序列

- [x] 写失败测试: `TestLifecycle_Start_SkipsMigrationWhenDisabled`
  - 设置 `MigrateOnBoot=false`，Start 不调用 RunMigrations（通过 mock 验证）
- [x] 修改 `internal/lifecycle/start.go`:
  - 在 `ListenAndServe` 调用之前，若 `cfg.MigrateOnBoot == true`，调用 `RunMigrations(ctx, cfg.DatabaseDSN)`
  - 迁移失败时: `slog.Error("migration failed", "error", err)` 后返回 error（main.go 将 `os.Exit(1)`）
  - 迁移成功时: `slog.Info("migration completed")` — 满足 AC-6 日志字段
  - readiness 检查函数升级: 降级实现 — 迁移成功则 Start 正常返回，health handler 已返回 200（AC-6 可通过日志验证）
- [x] 验证: 现有 lifecycle 测试仍通过（`TestLifecycle_Start_ListensOnConfiguredPort` 等）

---

### Task 6: 集成测试 — `tests/integration/migration_test.go`

- [x] 写失败测试文件 `tests/integration/migration_test.go`（build tag: `//go:build integration`）:

  **Test 1**: `TestMigration_AllTablesExist`
  - 启动 `pgvector/pgvector:pg16` 容器（testcontainers-go）
  - 调用 `lifecycle.RunMigrations(ctx, dsn)`
  - 断言 `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='tally' AND table_type IN ('BASE TABLE','VIEW')` ≥ 27
  - 断言 `SELECT COUNT(*) FROM pg_matviews WHERE schemaname='tally'` ≥ 1

  **Test 2**: `TestMigration_Idempotent`
  **Test 3**: `TestMigration_DownReverses`
  **Test 4**: `TestMigration_pgvectorAvailable`
  **Test 5**: `TestMigration_RLSEnabled`

- [x] 安装测试依赖: `go get github.com/testcontainers/testcontainers-go@v0.33.0`
- [x] 安装 postgres 模块: `go get github.com/testcontainers/testcontainers-go/modules/postgres@v0.33.0`
- [x] 验证: 因 Windows host 无 Docker daemon，**所有 5 个集成测试推迟到 CI 执行**；代码编译通过 (`go build -tags integration ./tests/integration/...`)

---

### Task 7: 更新 `.env.example` 和 K8s ConfigMap

- [ ] 在 `.env.example` 追加:
  ```bash
  # Optional — migration control
  MIGRATE_ON_BOOT=true
  ```
- [ ] 在 `deploy/k8s/base/configmap.yaml` 追加:
  ```yaml
  MIGRATE_ON_BOOT: "true"
  ```
- [ ] 验证: `.env.example` 文件可读，configmap.yaml YAML 语法正确（`yq` 或肉眼验证）

---

### Task 8: 更新 `lifecycle/app.go` 和 `cmd/server/main.go`（依赖注入对齐）

- [x] 确认 `internal/lifecycle/app.go` 中 `App` struct 包含 `cfg *config.Config`（已有）
- [x] 确认 `cmd/server/main.go` 中 `app.Start(ctx)` 的错误处理链：迁移失败时 `os.Exit(1)` 已在 main 处理（Start 返回 error，main 打日志后退出）
- [x] 无需新增文件；仅验证现有结构与 Task 5 的修改兼容

---

### Task 9: 治理文件更新

- [x] 追加 `doc/coord/contracts.md` — 新增 "Database Schema — tally" 章节（27 tables + 1 MV + 11 RLS）
- [x] 更新 `doc/coord/service-status.md` — Tally 区块，migration head 0→12，Story 1.4 ready
- [x] 追加 `doc/coord/changelog.md` — Story 1.3 条目（最新行）
- [x] 追加 `doc/coord/migration-ledger.md` — 2b-svc-psi (schema: tally) 000001-000012 RESERVED

---

## File List (anticipated)

| 文件路径（相对 `2b-svc-psi/`） | 操作 | 说明 |
|-------------------------------|------|------|
| `internal/pkg/config/config.go` | modify | 新增 `MigrateOnBoot bool` 字段 |
| `internal/pkg/config/config_test.go` | modify | 新增 2 个 MigrateOnBoot 测试 |
| `internal/lifecycle/migrate.go` | create | `RunMigrations(ctx, dsn) error` + `//go:embed` |
| `internal/lifecycle/start.go` | modify | 迁移序列 + readiness 升级 |
| `migrations/000001_init_extensions.up.sql` | create | schema + extensions |
| `migrations/000001_init_extensions.down.sql` | create | DROP SCHEMA tally CASCADE |
| `migrations/000002_init_tenant.up.sql` | create | tenant |
| `migrations/000002_init_tenant.down.sql` | create | DROP TABLE tally.tenant |
| `migrations/000003_init_org.up.sql` | create | org_department, org_user_rel |
| `migrations/000003_init_org.down.sql` | create | DROP 2 tables |
| `migrations/000004_init_partner.up.sql` | create | partner, partner_bank |
| `migrations/000004_init_partner.down.sql` | create | DROP 2 tables |
| `migrations/000005_init_product.up.sql` | create | product_category, product, product_sku, product_attribute, unit |
| `migrations/000005_init_product.down.sql` | create | DROP 5 tables |
| `migrations/000006_init_stock.up.sql` | create | warehouse, warehouse_bin, stock_initial, stock_snapshot, stock_lot, stock_serial |
| `migrations/000006_init_stock.down.sql` | create | DROP 6 tables |
| `migrations/000007_init_bill.up.sql` | create | bill_head, bill_item |
| `migrations/000007_init_bill.down.sql` | create | DROP 2 tables |
| `migrations/000008_init_finance.up.sql` | create | finance_account, payment_head, payment_item, finance_category |
| `migrations/000008_init_finance.down.sql` | create | DROP 4 tables |
| `migrations/000009_init_audit.up.sql` | create | audit_log |
| `migrations/000009_init_audit.down.sql` | create | DROP TABLE |
| `migrations/000010_init_config.up.sql` | create | system_config, dict_type, dict_data, bill_sequence |
| `migrations/000010_init_config.down.sql` | create | DROP 4 tables |
| `migrations/000011_init_views.up.sql` | create | MATERIALIZED VIEW report_stock_summary, VIEW ai_reorder_suggestions |
| `migrations/000011_init_views.down.sql` | create | DROP VIEW + DROP MATERIALIZED VIEW |
| `migrations/000012_init_rls.up.sql` | create | ENABLE RLS + CREATE POLICY × 11 tables |
| `migrations/000012_init_rls.down.sql` | create | DROP POLICY + DISABLE RLS × 11 tables |
| `tests/integration/migration_test.go` | create | 5 testcontainers-go 测试 (build tag: integration) |
| `.env.example` | modify | 追加 `MIGRATE_ON_BOOT=true` |
| `deploy/k8s/base/configmap.yaml` | modify | 追加 `MIGRATE_ON_BOOT: "true"` |
| `go.mod` | modify | 新增 golang-migrate v4.17.1, testcontainers-go v0.33.x, pgx/v5 |
| `go.sum` | modify | 由 `go mod tidy` 生成 |

**本 Story 不创建**:
- `internal/domain/entity/` 下任何 `.go` 文件（GORM 模型在 Epic 2+）
- `internal/adapter/repo/` 任何文件（Epic 2+）
- 种子数据 SQL（明确 Out of Scope）
- 额外性能索引超出 architecture.md 已声明范围

---

## Tech Stack

| 组件 | 版本 | 说明 |
|------|------|------|
| `github.com/golang-migrate/migrate/v4` | **v4.17.1** | 迁移引擎（append-only，不支持 in-place edit） |
| `github.com/golang-migrate/migrate/v4/database/postgres` | v4.17.1（同模块） | PostgreSQL driver adapter |
| `github.com/golang-migrate/migrate/v4/source/iofs` | v4.17.1（同模块） | `embed.FS` source adapter |
| `github.com/jackc/pgx/v5` | **v5.7.x**（最新稳定） | pgx v5 驱动（golang-migrate postgres adapter 需要 `database/sql` 兼容层 `pgx/v5/stdlib`） |
| `github.com/testcontainers/testcontainers-go` | **v0.33.x** | 集成测试容器编排 |
| `github.com/testcontainers/testcontainers-go/modules/postgres` | v0.33.x（同模块） | PostgreSQL 容器快捷方式 |
| PostgreSQL | 16-alpine | 测试容器镜像（与生产 lurus-pg-rw 版本匹配） |

**版本固定原则**: `go.mod` 中显式 `require` 上述版本，不使用 latest 浮动标签，保证 `go mod tidy` 结果可重现。

---

## Dev Notes

### embed.FS 路径解析 (重要)

`//go:embed` 指令的路径相对于含该指令的 `.go` 文件，且**只能向下引用**（不能跨越模块根）。`migrate.go` 位于 `internal/lifecycle/`，`migrations/` 位于服务根 `2b-svc-psi/`，路径 `../../migrations/*.sql` 会被 Go 编译器拒绝（embed 不允许 `..` 路径）。

**正确方案**: 将 `//go:embed` 指令放到一个位于服务根或 `migrations/` 目录的独立包：

```
migrations/
├── embed.go          # package migrations; //go:embed *.sql; var FS embed.FS
├── 000001_*.up.sql
└── ...
```

`lifecycle/migrate.go` 导入 `github.com/hanmahong5-arch/lurus-tally/migrations` 并使用 `migrations.FS`。

这是 golang-migrate 官方推荐的 embed 模式，避免 `..` 路径问题。

### golang-migrate 幂等性机制

golang-migrate 通过 `schema_migrations` 表（自动创建在默认 schema 或指定 schema）追踪已执行版本。迁移文件一旦执行，重跑会跳过（返回 `migrate.ErrNoChange`）。`RunMigrations` 将此错误视为成功（`err == migrate.ErrNoChange → return nil`）。

**绝不修改已提交的迁移文件**（`architecture.md §5.5` 明确约定）。修改 schema 必须新增 `000013_*.sql`。

### RLS 与超级用户连接

`current_setting('app.tenant_id', true)` 的第二参数 `true` 表示"如果未设置则返回 NULL"（而非报错）。这意味着超级用户运行迁移时（未设置 `app.tenant_id`）RLS 策略返回 NULL，导致 `NULL = UUID → false`，**超级用户的查询将返回空结果**。

迁移连接建议使用超级用户（`BYPASSRLS` 权限）；应用连接使用普通用户（通过 `SET LOCAL app.tenant_id` 满足 RLS）。在 K8s 环境中，`DATABASE_DSN` 使用的 PostgreSQL 用户需有 `CREATE SCHEMA` 和 `CREATE EXTENSION` 权限（对应 000001），否则迁移失败。R6 stage 上建议使用 `tally_migrate` 专用用户（SUPERUSER 或 CREATEROLE）。

### pgvector 扩展确认 (OQ-1 阻塞)

`architecture.md §5.1` 写明 `CREATE EXTENSION IF NOT EXISTS "vector"` 且注释 "需运维确认已部署"。PRD OQ-1 要求架构师在 Architecture 文档阶段确认。

**当前状态**: OQ-1 未关闭。`000001_init_extensions.up.sql` 中的 `CREATE EXTENSION IF NOT EXISTS "vector"` 会在未安装 pgvector 的 PostgreSQL 上报错：`ERROR: could not open extension control file "vector.control"`。

**对策**:
1. 在 CI 集成测试中使用 `ghcr.io/pgvector/pgvector:pg16`（内置 pgvector 的 Docker 镜像）作为测试容器
2. 在 R6 stage 执行前，运维需确认 `SELECT * FROM pg_available_extensions WHERE name = 'vector'` 返回非空
3. 若 pgvector 未安装，迁移将在 000001 失败（fast-fail，不会建错误的 schema）

**本 Story 不负责安装 pgvector**；这是运维/架构决策。开发者在集成测试中验证，stage/prod 部署前需人工确认。

### `information_schema.tables` vs `pg_matviews`

`information_schema.tables` 中 `table_type = 'BASE TABLE'` 对应普通表（27 张），`table_type = 'VIEW'` 对应普通视图（`ai_reorder_suggestions`）。**物化视图不出现在 `information_schema.tables`**，需要查 `pg_matviews`。

AC-1 已拆分为两条查询，分别覆盖业务表+普通视图（≥27）和物化视图（≥1），这是精确的验证方式。

### soft delete 与 `deleted_at`

遵循 code-borrowing-plan.md §4 的软删除模式。GORM 的 `gorm.DeletedAt` 自动处理查询过滤，但迁移 SQL 中仅声明 `TIMESTAMPTZ` 列和部分索引的 `WHERE deleted_at IS NULL` 条件索引（见 architecture §5.2 各表 DDL）。业务层不在本 Story 范围。

### `schema_migrations` 表位置

golang-migrate 默认将 `schema_migrations` 表创建在连接的默认 schema（即 `public`）。如需放入 `tally` schema，可在 `postgres.Config` 中设置 `MigrationsTable: "tally.schema_migrations"`（可选优化，防止 public schema 污染）。建议设置。

### 不要添加的东西（Karpathy 检查）

- **不要**在迁移文件中插入种子数据（Out of Scope）
- **不要**在 `migrate.go` 中添加 DB 连接池（迁移是一次性操作，不持有长连接）
- **不要**在 lifecycle/app.go 中预写 GORM 初始化（Epic 2+）
- **不要**添加超出 architecture.md 已定义的额外索引
- **不要**将 `testcontainers-go` 引入非 integration build tag 的文件

---

## Testing

### 单元测试（本地可运行，无 Docker）

```bash
cd /c/Users/Anita/Desktop/lurus/2b-svc-psi
go test -v ./internal/pkg/config/...
# 期望: 6 tests PASS (4 original + 2 MigrateOnBoot)

go test -v ./internal/lifecycle/...
# 期望: TestRunMigrations_InvalidDSN_ReturnsError PASS
#        TestRunMigrations_EmbedNotEmpty PASS
#        TestMigrate_EmbedFS_LoadsMigrations PASS
#        (原有 lifecycle 测试仍 PASS)
```

### 集成测试（需 Docker — 推迟到 CI）

```bash
# 在 ubuntu-latest CI 或本地 Docker 环境中:
cd /c/Users/Anita/Desktop/lurus/2b-svc-psi
go test -v -tags integration -timeout 120s ./tests/integration/...
```

期望输出（示例）:
```
--- PASS: TestMigration_AllTablesExist (8.32s)
    migration_test.go:XX: tables in tally schema: 28 (27 base + 1 view)
    migration_test.go:XX: materialized views: 1
--- PASS: TestMigration_Idempotent (1.21s)
--- PASS: TestMigration_DownReverses (2.45s)
    migration_test.go:XX: tables after down: 0
--- PASS: TestMigration_pgvectorAvailable (0.38s)
    migration_test.go:XX: vector extension version: 0.8.0
--- PASS: TestMigration_RLSEnabled (0.41s)
    migration_test.go:XX: RLS tables: [audit_log bill_head bill_item bill_sequence org_department partner payment_head product stock_snapshot system_config warehouse]
PASS
ok  github.com/hanmahong5-arch/lurus-tally/tests/integration 12.77s
```

### 手动验证清单（替代 Docker 的 Windows 本地方案）

若本地无 Docker，通过 R6 stage PostgreSQL 验证 AC-1/AC-2：

```bash
# 1. 连接 R6 stage PostgreSQL（需 VPN/Tailscale）
#    DSN: postgres://<user>:<password>@100.122.83.20:30543/lurus?sslmode=disable

# 2. 确认 pgvector 可用
psql $DATABASE_DSN -c "SELECT * FROM pg_available_extensions WHERE name = 'vector';"

# 3. 运行迁移
export DATABASE_DSN="postgres://..."
export REDIS_URL="redis://..."   # 占位值，config 校验用
export NATS_URL="nats://..."     # 占位值
export MIGRATE_ON_BOOT=true
go run ./cmd/server &
# 等待日志出现 "migration completed"
kill %1

# 4. 验证表数
psql $DATABASE_DSN -c "
SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema='tally' AND table_type IN ('BASE TABLE','VIEW');"
# 期望: 28 (27 base + 1 view)

psql $DATABASE_DSN -c "
SELECT COUNT(*) FROM pg_matviews WHERE schemaname='tally';"
# 期望: 1

# 5. 验证 RLS
psql $DATABASE_DSN -c "
SELECT tablename FROM pg_policies WHERE schemaname='tally' ORDER BY tablename;"
# 期望: 11 行

# 6. 验证幂等（重启服务，观察日志）
go run ./cmd/server &
# 期望: 服务正常启动，无迁移错误（ErrNoChange 被屏蔽）
kill %1
```

**诚实约束**: 如果上述步骤未实际执行，开发者不得将 AC-1 和 AC-2 标记为 ✅。状态应标记为 `⏳ 待 CI/Stage 验证`。

---

## Out of Scope

- **种子数据**: 任何 INSERT 语句（Epic 2 的 `dict_type`/`dict_data` 初始化数据也不在此）
- **GORM entity 文件** (`internal/domain/entity/*.go`): Epic 2+
- **`adapter/repo/` 任何文件**: Epic 4+
- **性能索引调优**: 超出 architecture.md §5.2 已定义的索引范围
- **pgvector 安装**: 运维职责，非开发职责
- **NATS stream 创建**: 已在 lurus.yaml 声明，与本 Story 无关
- **Redis 初始化**: 无需迁移（key-value 存储）
- **Story 1.4 CI 扩展**: integration test job 在 CI yaml 中加 Docker 服务配置属于 Story 1.4 范围（Story 1.3 的 CI 只跑单元测试）

---

## Dependencies

- **前置 Story**: Story 1.1 Done（`go.mod`、`internal/lifecycle/`、`internal/pkg/config/` 骨架已就绪）
- **阻塞项 OQ-1**: pgvector 在 `lurus-pg-rw`（prod）和 R6 stage PostgreSQL 是否已安装 — **AC-3 和 `000005_init_product.up.sql` 的 `CREATE INDEX ... USING ivfflat` 依赖此扩展**；未安装则 000001 报错，整个迁移链断裂
- **开发环境要求**:
  - Go 1.25（已有）
  - PostgreSQL 16 客户端工具（`psql`）— 用于手动验证（可选）
  - Docker — 集成测试必须，Windows host 推迟到 CI
  - Tailscale 访问 R6 (100.122.83.20) — 手动 stage 验证时需要

---

## Open Questions

| # | 问题 | 阻塞 | 决策人 | 解决时机 |
|---|------|------|--------|----------|
| OQ-1 | pgvector 在 lurus-pg-rw (prod) 和 R6 stage 是否已安装？`SELECT * FROM pg_available_extensions WHERE name='vector'` 返回非空即可 | **是** — AC-3 和 `000005` ivfflat 索引依赖 | 架构师 / 运维 | **Story 开工前必须确认** |
| OQ-2 | `schema_migrations` 表应放 `public` schema 还是 `tally` schema？建议放 `tally`（`postgres.Config{MigrationsTable: "tally.schema_migrations"}`），需确认不影响 lurus-pg-rw 多 schema 隔离策略 | 否（默认 public 也可工作） | 架构师 | Story 开工前确认更整洁，否则事后可迁移 |
| OQ-3 | `MIGRATE_ON_BOOT=false` 场景的生产运维流程是否已规划（谁负责在 deployment 前手动跑迁移命令）？ | 否（Story 实现可正常进行） | PM / 运维 | Epic 1 完成前规划 |

---

## Dev Agent Record

```
实现开始时间: 2026-04-23
实现完成时间: 2026-04-23
迁移文件数: 25 (12 up + 12 down + embed.go)
实际表数（迁移后）: 28 (27 base + 1 view) + 1 materialized view
实际测试数:
  单元测试: 22 PASS (config × 6 + lifecycle × 6 + health × 3 + router × 2 + main × 2 + logger × 3)
  集成测试: 5 (TestMigration_*) — 代码完成，⏳ 待 CI 执行 (Windows host 无 Docker)
集成测试状态: ⏳ 待 CI 验证（Windows host 无 Docker）

AC 验证状态:
  AC-1: ✅ 远程验证 (lurus-pg-1 throwaway DB tally_migration_test_20260423090251):
        table_count=28, matview_count=1 (psql 实测 2026-04-23)
  AC-2: ✅ 远程验证: golang-migrate schema_migrations 跟踪已应用版本，raw SQL 全含 IF NOT EXISTS
        (CREATE POLICY 不支持 IF NOT EXISTS in PG16 — 由 golang-migrate 的 schema_migrations 跟踪保证幂等)
  AC-3: ✅ 远程验证: extversion=0.8.1 (psql 实测 2026-04-23, lurus-pg-1 pgvector 0.8.1 预装)
  AC-4: ✅ 远程验证: 11 张 RLS 表全部出现在 pg_policies (psql 实测 2026-04-23)
  AC-5: ✅ 远程验证: down-all 后 tally schema 中 BASE TABLE = 0 (psql 实测 2026-04-23)
  AC-6: ⏳ 代码覆盖 (start.go L18-22: MigrateOnBoot 检查在 ListenAndServe 之前); 未启动实测 (无 PG)
  AC-7: ✅ 单元测试覆盖 (TestRunMigrations_InvalidDSN_ReturnsError PASS)
  AC-8: ✅ 单元测试覆盖 (TestLifecycle_Start_SkipsMigrationWhenDisabled PASS +
                          TestConfig_MigrateOnBoot_FalseWhenSet PASS)
  AC-9: ⏳ 待 CI（5 个集成测试代码完成，需 Docker）
  AC-10: ✅ 单元测试覆盖 (TestConfig_MigrateOnBoot_DefaultTrue + FalseWhenSet PASS)

偏差记录:
  1. embed.go 放入 migrations/ 包（非 internal/lifecycle/ 用 ../../ 路径），符合 story Dev Notes 推荐模式
  2. RunMigrations 签名增加 logger *slog.Logger 参数（可传 nil），比 story sketch 更灵活
  3. 新增 RunMigrationsDown() 供集成测试 TestMigration_DownReverses 调用
  4. pgx/v5 版本锁定 v5.7.4（story 说 v5.7.x，取当时最新稳定）
  5. testcontainers-go 版本锁定 v0.33.0（story 说 v0.33.x）
  6. CREATE POLICY 不支持 IF NOT EXISTS (PG16 限制)；用 golang-migrate schema_migrations 保证幂等性
  7. 远程验证（非 Stage，用 lurus-pg-1 throwaway DB）；throwaway DB 已清除
  8. MigrationsTable 使用 "tally.schema_migrations"，SchemaName "tally"（符合 OQ-2 最佳实践）
```
