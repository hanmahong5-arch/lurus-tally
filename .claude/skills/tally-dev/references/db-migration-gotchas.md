> 从 SKILL.md 拆出的深度材料(2026-07-01 渐进披露批次2)。§4 Migration / DB 陷阱(scratch embed / pg_trgm / dirty / NUMERIC / 数组 / RLS nil UUID)。

## 4. Migration / DB 陷阱

### 4.1 Scratch image 必须 //go:embed

Tally Dockerfile 用 scratch base — 没文件系统。所有运行时读的文件都要 embed：

```go
// migrations/data/embed.go
package migrationdata

import "embed"

//go:embed *.sql
var FS embed.FS
```

```go
// 使用：
data, err := migrationdata.FS.ReadFile("nursery_seed.sql")
```

S28.1 部署时 backend log "no such file or directory" → 加 `//go:embed` 修。**任何 SQL seed / template / fixture 都必须 embed**。

### 4.2 pg_trgm op class 必须 schema 限定

若目标环境的 `pg_trgm` 扩展装在 `tally` schema 而非 public（例如 `CREATE EXTENSION pg_trgm SCHEMA tally`），Migration DDL 写裸 `gin_trgm_ops` 会失败，必须 `tally.gin_trgm_ops`：

```sql
-- WRONG: gin_trgm_ops does not exist
CREATE INDEX idx_x_name_trgm ON tally.x USING GIN (name gin_trgm_ops);

-- RIGHT
CREATE INDEX idx_x_name_trgm ON tally.x USING GIN (name tally.gin_trgm_ops);
```

S28.1 (028) 巧合成功（应用时 search_path 设了 tally），S28.2 (029) 直接失败。**永远写全限定**。

### 4.3 schema_migrations dirty 标志 + force fix

migration runner（golang-migrate）每次执行先标 dirty=true，成功才标 false。手动应用 SQL 失败会留 dirty 痕迹，pod 启动时 migrator 拒绝再跑：

```
ERROR: Dirty database version 29. Fix and force version.
```

修复：
```bash
# 在你的 Postgres 实例上手动清 dirty 标志（版本号替换成实际卡住的那个）：
psql -U postgres -d tally -c "UPDATE tally.schema_migrations SET dirty=false WHERE version=29"
# K8s 部署：强删 CrashLoop pod 让 deployment 起新的
kubectl -n <your-namespace> delete pod <crashing-pod> --grace-period=0 --force
# Docker Compose 部署：重启 backend 容器即可（见 deploy/customer/INSTALL.md）
docker compose -f deploy/customer/docker-compose.customer.yml restart backend
```

### 4.4 NUMERIC(18,2) scan 用 *string

Tally 不用 ORM，用 `database/sql` raw SQL。NUMERIC 列没有原生 Go 类型映射 — 看现有 repo（`internal/adapter/repo/bill/`）用 `*string` 然后业务层转 `decimal` 或 `big.Rat`。**新模块照抄不要发明**。

### 4.5 TEXT[] / INT[] 用 manual sliceToArray

Tally **不用 lib/pq.Array**（依赖被裁掉了），用 manual string conversion：
```go
// internal/adapter/repo/horticulture/dict_repo.go (参考)
func sliceToArray(s []string) string { /* "{a,b,c}" */ }
func intSliceToArray(s []int) string  { /* "{1,2}" */ }
```
零值 `[0,0]` 存为 `'{}'`（空数组），别存 `'{0,0}'`。

### 4.6 RLS shared seed 用 nil UUID

苗木字典等 reference data 跨租户共享：行的 `tenant_id = '00000000-0000-0000-0000-000000000000'` (nil UUID)。RLS policy:
```sql
USING (
    tenant_id = current_setting('app.tenant_id', true)::UUID
    OR tenant_id = '00000000-0000-0000-0000-000000000000'::UUID
)
```
**别用 `tenant_id IS NULL`** — RLS 里 NULL 比较静默排除。

