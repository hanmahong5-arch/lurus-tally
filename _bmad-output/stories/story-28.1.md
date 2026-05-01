# Story 28.1: 苗木字典表 + 基础 CRUD + 200 种初始化包

**Epic**: E28 — 项目制核算 + 多维度汇总 + 苗木字典 + 价格分级
**Story ID**: 28.1
**Priority**: P0 (Epic 28 first story — blocks 28.2, 28.3, 28.9, 28.13, E29.5/29.6, E31.1/31.2)
**Type**: full-stack (migration + Go domain/app/repo/handler + Next.js list+form pages + seed SQL)
**Estimate**: 16h total (see per-task estimates)
**Status**: Done

---

## Context

Tally's first vertical-industry landing is the horticulture/nursery sector. All subsequent Epic 28
stories (project costing, price grading, inquiry, loss tracking) require a canonical nursery species
dictionary to attach SKUs, prices, and project line items to. Without it, users must free-type
species names on every entry — no autocomplete, no shared price history, no seasonality metadata.

Story 28.1 delivers the `tally.nursery_dict` table (migration 028), the full Go domain/app/repo/
handler stack following the identical layered pattern established by `internal/{domain,app,adapter}/
product/`, and the Next.js list + new/edit form pages. A seed SQL file pre-populates 200 common
nursery species drawn from public sources (Chinese Flora / 中国植物志), controlled by
`SEED_NURSERY_DICT=true` so it never pollutes automated test environments.

After this story, a first-time user can open `/dictionary`, type "红枫" in the search box, and
immediately see 8 candidate species with Latin name, family, type, and spec template — matching
the "开箱即用" promise in HD-8 (architecture decision record).

---

## Acceptance Criteria

1. `GET /api/v1/nursery-dict?q=红枫&limit=20` returns HTTP 200 with a JSON array containing at
   least 8 items whose `name` field contains "红枫", each with non-empty `latin_name`, `family`,
   `type`, and `spec_template` fields.

2. When the seed file has been applied (`SEED_NURSERY_DICT=true`), `GET /api/v1/nursery-dict?
   limit=200&offset=0` returns `total >= 200`; all 200 entries are visible in the `/dictionary`
   list page with pagination (default page size 20).

3. Creating two `nursery_dict` entries with the same `name` under the same `tenant_id` returns
   HTTP 409 with `{"error":"duplicate name"}`. The unique constraint `unique(tenant_id, name)` is
   enforced at both the database layer and the use case layer (returns `ErrDuplicateName`).

4. A soft-deleted entry (via `DELETE /api/v1/nursery-dict/:id`) is no longer returned by
   `GET /api/v1/nursery-dict` (list excludes `deleted_at IS NOT NULL`). Calling
   `POST /api/v1/nursery-dict/:id/restore` returns HTTP 200 and the entry reappears in the list.

5. All six REST endpoints respond within their expected status codes: GET list 200, POST create
   201 (with Location header), GET single 200/404, PUT update 200/404, DELETE soft-delete 204,
   POST restore 200. A request with a valid auth token but a non-existent `id` returns 404.

6. The `/dictionary` Next.js page renders without JavaScript errors in the browser. The page
   shows a search input, a table with columns Name / Latin Name / Type / Family / Is Evergreen /
   Default Unit / Actions, and a "+ 新增苗木" button. Clicking a row opens the detail drawer/modal
   showing all fields including `spec_template` rendered as a key-value list.

7. Row-level security is enforced: a tenant can only read and write its own `nursery_dict` rows.
   The shared-seed rows (inserted with a sentinel `tenant_id = '00000000-0000-0000-0000-000000000000'`)
   are readable by all tenants (RLS policy uses `tenant_id = current_setting('app.tenant_id')::UUID
   OR tenant_id = '00000000-0000-0000-0000-000000000000'`). A tenant-specific entry created by
   Tenant A is not visible when querying as Tenant B.

8. `go test ./internal/domain/horticulture/... ./internal/app/horticulture/... ./internal/adapter/
   repo/horticulture/... ./internal/adapter/handler/horticulture/... -v -count=1 -race` exits 0.
   `cd web && bun run test` exits 0. `CGO_ENABLED=0 GOOS=linux go build ./...` and
   `bun run build` both exit 0 after this story.

---

## Tasks / Subtasks

### Task 1: Migration 028 — create `tally.nursery_dict` table (1.5h)

- [ ] Write failing test: `migrations/000028_nursery_dict.up.sql` does not yet exist — confirm
  with `ls migrations/ | grep 000028`; assert it is absent before creating.
- [ ] Create `migrations/000028_nursery_dict.up.sql`:

  ```sql
  CREATE TYPE tally.nursery_type AS ENUM
      ('tree','shrub','herb','vine','bamboo','aquatic','bulb','fruit');

  CREATE TABLE tally.nursery_dict (
      id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
      tenant_id        UUID         NOT NULL,
      name             VARCHAR(100) NOT NULL,
      latin_name       VARCHAR(200),
      family           VARCHAR(100),
      genus            VARCHAR(100),
      type             tally.nursery_type NOT NULL DEFAULT 'tree',
      is_evergreen     BOOLEAN      NOT NULL DEFAULT false,
      climate_zones    TEXT[]       NOT NULL DEFAULT '{}',
      -- best_season: 2-element array [start_month, end_month], months 1-12
      best_season      INT[]        NOT NULL DEFAULT '{}',
      -- spec_template: JSONB dict of typical spec keys, e.g.
      -- {"胸径_cm": null, "冠幅_cm": null, "高度_cm": null}
      spec_template    JSONB        NOT NULL DEFAULT '{}',
      default_unit_id  UUID         REFERENCES tally.unit_def(id) ON DELETE SET NULL,
      photo_url        TEXT,
      remark           TEXT,
      created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
      updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
      deleted_at       TIMESTAMPTZ,
      CONSTRAINT uq_nursery_dict_tenant_name UNIQUE (tenant_id, name)
  );

  CREATE INDEX idx_nursery_dict_tenant   ON tally.nursery_dict(tenant_id);
  CREATE INDEX idx_nursery_dict_type     ON tally.nursery_dict(tenant_id, type);
  CREATE INDEX idx_nursery_dict_name_trgm ON tally.nursery_dict
      USING GIN (name gin_trgm_ops);
  CREATE INDEX idx_nursery_dict_spec_gin ON tally.nursery_dict
      USING GIN (spec_template);

  -- RLS: own rows + shared seed rows (tenant_id = nil UUID = public seed)
  ALTER TABLE tally.nursery_dict ENABLE ROW LEVEL SECURITY;
  CREATE POLICY nursery_dict_rls ON tally.nursery_dict
      USING (
          tenant_id = current_setting('app.tenant_id', true)::UUID
          OR tenant_id = '00000000-0000-0000-0000-000000000000'::UUID
      );
  ```

  Note: `pg_trgm` extension was enabled in migration 000001; confirm before relying on
  `gin_trgm_ops` index. If unavailable, fall back to a B-tree index on `name` (still supports
  `ILIKE '%query%'` scan, just slower).

- [ ] Create `migrations/000028_nursery_dict.down.sql`:

  ```sql
  DROP TABLE IF EXISTS tally.nursery_dict;
  DROP TYPE IF EXISTS tally.nursery_type;
  ```

- [ ] Verify: `make migrate-up` applies without error against local dev Postgres.

---

### Task 2: Go domain layer — `internal/domain/horticulture/dict.go` (1h)

- [ ] Write failing test: `internal/domain/horticulture/dict_test.go`:
  - `TestNurseryDict_Validate_RejectsEmptyName` — construct a `NurseryDict` with `Name=""`;
    call `d.Validate()`; assert error contains "name is required".
  - `TestNurseryDict_Validate_RejectsBadSeason` — `BestSeason = [13, 2]`; assert error contains
    "best_season month must be between 1 and 12".
  - `TestNurseryType_String_AllValuesRoundtrip` — range over all eight `NurseryType` constants,
    call `String()`, parse back via `ParseNurseryType`, assert equality.
- [ ] Create `internal/domain/horticulture/dict.go`:

  ```go
  package horticulture

  import (
      "encoding/json"
      "fmt"
      "time"
      "github.com/google/uuid"
  )

  // NurseryType classifies a plant species.
  type NurseryType string

  const (
      NurseryTypeTree    NurseryType = "tree"
      NurseryTypeShrub   NurseryType = "shrub"
      NurseryTypeHerb    NurseryType = "herb"
      NurseryTypeVine    NurseryType = "vine"
      NurseryTypeBamboo  NurseryType = "bamboo"
      NurseryTypeAquatic NurseryType = "aquatic"
      NurseryTypeBulb    NurseryType = "bulb"
      NurseryTypeFruit   NurseryType = "fruit"
  )

  // ParseNurseryType converts a raw string to NurseryType with validation.
  func ParseNurseryType(s string) (NurseryType, error)

  // NurseryDict is the canonical species record in the nursery dictionary.
  // tenant_id = uuid.Nil denotes a shared seed entry visible to all tenants.
  type NurseryDict struct {
      ID            uuid.UUID
      TenantID      uuid.UUID
      Name          string
      LatinName     string
      Family        string
      Genus         string
      Type          NurseryType
      IsEvergreen   bool
      ClimateZones  []string
      BestSeason    [2]int          // [start_month, end_month]; zero value means unset
      SpecTemplate  json.RawMessage // {"胸径_cm": null, "冠幅_cm": null}
      DefaultUnitID *uuid.UUID
      PhotoURL      string
      Remark        string
      CreatedAt     time.Time
      UpdatedAt     time.Time
      DeletedAt     *time.Time
  }

  // Validate enforces domain invariants.
  func (d *NurseryDict) Validate() error

  // CreateInput carries fields for creating a new NurseryDict.
  type CreateInput struct {
      TenantID      uuid.UUID
      Name          string
      LatinName     string
      Family        string
      Genus         string
      Type          NurseryType
      IsEvergreen   bool
      ClimateZones  []string
      BestSeason    [2]int
      SpecTemplate  json.RawMessage
      DefaultUnitID *uuid.UUID
      PhotoURL      string
      Remark        string
  }

  // UpdateInput carries mutable fields.
  type UpdateInput struct {
      Name          *string
      LatinName     *string
      Family        *string
      Genus         *string
      Type          *NurseryType
      IsEvergreen   *bool
      ClimateZones  []string
      BestSeason    *[2]int
      SpecTemplate  json.RawMessage
      DefaultUnitID *uuid.UUID
      PhotoURL      *string
      Remark        *string
  }

  // ListFilter controls list queries.
  type ListFilter struct {
      TenantID    uuid.UUID
      Query       string      // ILIKE on name
      Type        *NurseryType
      IsEvergreen *bool
      Limit       int
      Offset      int
  }
  ```

- [ ] Verify: `go test ./internal/domain/horticulture/... -v` passes.

---

### Task 3: Go app layer — repository interface + use cases (2h)

- [ ] Write failing test: `internal/app/horticulture/dict_usecases_test.go` (table-driven,
  using a hand-written fake `Repository`):
  - `TestCreateUseCase_Execute_ReturnsDuplicateNameError` — fake repo returns
    `ErrDuplicateName`; assert use case surfaces the same error.
  - `TestCreateUseCase_Execute_HappyPath` — fake repo Create succeeds; assert returned
    `NurseryDict.Name` matches input.
  - `TestDeleteUseCase_Execute_SetsDeletedAt` — fake repo Delete succeeds; assert no error.
  - `TestDeleteUseCase_Execute_NotFoundError` — fake repo returns `ErrNotFound`; assert surfaced.
  - `TestRestoreUseCase_Execute_HappyPath` — fake repo Restore succeeds; assert returned item
    has `DeletedAt == nil`.
  - `TestListUseCase_Execute_DefaultsLimit` — call with `Limit=0`; assert fake repo received
    `Limit=20`.
- [ ] Create `internal/app/horticulture/repository.go`:

  ```go
  package horticulture

  import (
      "context"
      "errors"
      "github.com/google/uuid"
      domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
  )

  // ErrNotFound is returned when the requested nursery dict entry does not exist.
  var ErrNotFound = errors.New("nursery dict entry not found")

  // ErrDuplicateName is returned when a name already exists for the tenant.
  var ErrDuplicateName = errors.New("nursery dict duplicate name")

  // Repository abstracts the persistence layer for NurseryDict.
  type Repository interface {
      Create(ctx context.Context, d *domain.NurseryDict) error
      GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.NurseryDict, error)
      List(ctx context.Context, f domain.ListFilter) ([]*domain.NurseryDict, int, error)
      Update(ctx context.Context, d *domain.NurseryDict) error
      Delete(ctx context.Context, tenantID, id uuid.UUID) error
      Restore(ctx context.Context, tenantID, id uuid.UUID) (*domain.NurseryDict, error)
  }
  ```

- [ ] Create `internal/app/horticulture/create.go` — `CreateUseCase.Execute` validates input,
  checks name uniqueness error from repo, constructs a `NurseryDict` with a new UUID, calls
  `repo.Create`, returns the created entity.
- [ ] Create `internal/app/horticulture/get.go` — `GetByIDUseCase.Execute` delegates to
  `repo.GetByID`.
- [ ] Create `internal/app/horticulture/list.go` — `ListUseCase.Execute` enforces
  `Limit` in range `[1, 200]` defaulting to 20, delegates to `repo.List`.
- [ ] Create `internal/app/horticulture/update.go` — `UpdateUseCase.Execute` fetches the
  existing entry, applies non-nil fields from `UpdateInput`, validates, calls `repo.Update`.
- [ ] Create `internal/app/horticulture/delete.go` — `DeleteUseCase.Execute` delegates to
  `repo.Delete`; returns `ErrNotFound` if repo does.
- [ ] Create `internal/app/horticulture/restore.go` — `RestoreUseCase.Execute` delegates to
  `repo.Restore`; returns `ErrNotFound` if repo does.
- [ ] Verify: `go test ./internal/app/horticulture/... -v -race` passes.

---

### Task 4: Go adapter/repo — `internal/adapter/repo/horticulture/dict_repo.go` (2h)

- [ ] Write failing test: `internal/adapter/repo/horticulture/dict_repo_test.go`
  (integration-tagged, requires a live Postgres via testcontainers or a `TEST_DSN` env var;
  follow the same test pattern as existing product or bill repo tests):
  - `TestDictRepo_Create_HappyPath` — create one entry; assert row exists via `GetByID`.
  - `TestDictRepo_Create_DuplicateNameReturnsError` — create same name twice; assert
    `errors.Is(err, apphort.ErrDuplicateName)`.
  - `TestDictRepo_List_FiltersOnQuery` — insert "红枫A" and "银杏B"; list with `Query="红枫"`;
    assert len==1.
  - `TestDictRepo_Delete_SoftDeletesRow` — create then delete; `GetByID` returns `ErrNotFound`.
  - `TestDictRepo_Restore_MakesRowVisibleAgain` — soft-delete then restore; `GetByID` succeeds.
  - `TestDictRepo_RLS_TenantBCannotSeeTenantARow` — create with tenantA; query with tenantB
    session; assert 0 rows returned.
  - `TestDictRepo_SeedRows_VisibleToAllTenants` — insert row with tenant_id=uuid.Nil; query
    with any tenant; assert row appears in list.
- [ ] Create `internal/adapter/repo/horticulture/dict_repo.go` following the same pattern as
  `internal/adapter/repo/product/repo.go`:
  - Use `database/sql` + raw SQL (no ORM); `DB` interface matching the existing product repo
    pattern.
  - `Create`: INSERT with all columns; detect `pq.ErrorCode 23505` (unique_violation) and
    return `apphort.ErrDuplicateName`.
  - `GetByID`: SELECT WHERE `id=$1 AND (tenant_id=$2 OR tenant_id='00000000-...')
    AND deleted_at IS NULL`.
  - `List`: dynamic WHERE builder; include `tenant_id=$1 OR tenant_id='00000000-...'`;
    ILIKE on `name` when `Query` is set; filter on `type` and `is_evergreen` if set;
    ORDER BY `name ASC`; returns total count + paginated slice.
  - `Update`: UPDATE where `id=$1 AND tenant_id=$2 AND deleted_at IS NULL`; return
    `ErrNotFound` on 0 rows affected.
  - `Delete`: UPDATE SET `deleted_at=now()` WHERE `id=$1 AND tenant_id=$2 AND
    deleted_at IS NULL`; return `ErrNotFound` on 0 rows.
  - `Restore`: UPDATE SET `deleted_at=NULL, updated_at=now()` WHERE `id=$1 AND
    tenant_id=$2 AND deleted_at IS NOT NULL`; re-fetch and return.
  - `scanDict(rowScanner)` helper scans all columns; handle `NULL` for optional fields.
- [ ] Verify: `go test -tags integration ./internal/adapter/repo/horticulture/... -v` passes
  (requires `TEST_DSN` env var pointing to a local Postgres with tally schema and migration 028
  applied).

---

### Task 5: Go adapter/handler — `internal/adapter/handler/horticulture/dict_handler.go` (2h)

- [ ] Write failing test: `internal/adapter/handler/horticulture/dict_handler_test.go`
  (unit test using `httptest`, fake use cases injected via interfaces):
  - `TestDictHandler_List_Returns200WithItems` — inject list UC returning 3 items; GET /api/v1/
    nursery-dict; assert 200, `"total":3`, `"items"` array len 3.
  - `TestDictHandler_Create_Returns201` — valid JSON body; assert 201 + Location header.
  - `TestDictHandler_Create_DuplicateName_Returns409` — UC returns `ErrDuplicateName`; assert 409.
  - `TestDictHandler_GetByID_Returns404ForUnknown` — UC returns `ErrNotFound`; assert 404.
  - `TestDictHandler_Update_Returns200` — UC returns updated entity; assert 200.
  - `TestDictHandler_Delete_Returns204` — UC succeeds; assert 204.
  - `TestDictHandler_Restore_Returns200` — UC returns restored entity; assert 200.
- [ ] Create `internal/adapter/handler/horticulture/dict_handler.go`:

  Request/response shapes (flat JSON, no nested union):

  ```go
  // ListResponse wraps pagination envelope.
  type ListResponse struct {
      Items []*NurseryDictDTO `json:"items"`
      Total int               `json:"total"`
  }

  // NurseryDictDTO is the wire representation (no internal UUIDs for other tenants).
  type NurseryDictDTO struct {
      ID            string          `json:"id"`
      TenantID      string          `json:"tenant_id"`
      Name          string          `json:"name"`
      LatinName     string          `json:"latin_name"`
      Family        string          `json:"family"`
      Genus         string          `json:"genus"`
      Type          string          `json:"type"`
      IsEvergreen   bool            `json:"is_evergreen"`
      ClimateZones  []string        `json:"climate_zones"`
      BestSeason    [2]int          `json:"best_season"`
      SpecTemplate  json.RawMessage `json:"spec_template"`
      DefaultUnitID *string         `json:"default_unit_id,omitempty"`
      PhotoURL      string          `json:"photo_url"`
      Remark        string          `json:"remark"`
      CreatedAt     time.Time       `json:"created_at"`
      UpdatedAt     time.Time       `json:"updated_at"`
  }
  ```

  Routes registered via `RegisterRoutes(rg *gin.RouterGroup)`:
  - `GET    /nursery-dict`          → list (query params: `q`, `type`, `is_evergreen`, `limit`, `offset`)
  - `POST   /nursery-dict`          → create
  - `GET    /nursery-dict/:id`      → get by ID
  - `PUT    /nursery-dict/:id`      → update
  - `DELETE /nursery-dict/:id`      → soft-delete
  - `POST   /nursery-dict/:id/restore` → restore

  Error mapping:
  - `ErrNotFound` → 404
  - `ErrDuplicateName` → 409 `{"error":"duplicate name"}`
  - `domain.Validate` errors → 400
  - all others → 500

- [ ] Verify: `go test ./internal/adapter/handler/horticulture/... -v -race` passes;
  `CGO_ENABLED=0 GOOS=linux go build ./...` exits 0.

---

### Task 6: Router registration + DI wiring (1h)

- [ ] Write failing test: `internal/adapter/handler/router/router_test.go` already exists.
  Add assertions:
  - `TestRouter_RegistersNurseryDictRoutes` — call `router.New(...)` with a real
    `*handlerhorticulture.DictHandler`; assert routes `GET /api/v1/nursery-dict` and
    `POST /api/v1/nursery-dict/:id/restore` are registered (use `engine.Routes()` to enumerate).
- [ ] Modify `internal/adapter/handler/router/router.go`:
  - Import `handlerhorticulture "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/horticulture"`.
  - Add `dh *handlerhorticulture.DictHandler` parameter to `New(...)`.
  - Inside the `api` group block:

    ```go
    // Horticulture — nursery dictionary (Story 28.1)
    if dh != nil {
        dh.RegisterRoutes(api)
    } else {
        api.GET("/nursery-dict", notImplemented)
        api.POST("/nursery-dict", notImplemented)
    }
    ```

- [ ] Modify `internal/lifecycle/app.go`:
  - Construct the dependency chain:
    1. `dictRepo   := hortrepo.New(db)`
    2. `createUC   := hortapp.NewCreateUseCase(dictRepo)`
    3. `getUC      := hortapp.NewGetByIDUseCase(dictRepo)`
    4. `listUC     := hortapp.NewListUseCase(dictRepo)`
    5. `updateUC   := hortapp.NewUpdateUseCase(dictRepo)`
    6. `deleteUC   := hortapp.NewDeleteUseCase(dictRepo)`
    7. `restoreUC  := hortapp.NewRestoreUseCase(dictRepo)`
    8. `dictHandler := horthandler.NewDictHandler(createUC, getUC, listUC, updateUC, deleteUC, restoreUC)`
  - Pass `dictHandler` to `router.New(...)`.
- [ ] Verify: `go build ./... ` exits 0; existing `lifecycle_test.go` still passes.

---

### Task 7: Seed data — `migrations/data/nursery_seed.sql` (2h)

The seed file is applied separately (not embedded in the main migration binary), controlled by
`SEED_NURSERY_DICT=true`. The dev agent must insert exactly 200 rows using public botanical data
(Chinese Flora / 中国植物志 samples are acceptable). All rows use `tenant_id =
'00000000-0000-0000-0000-000000000000'`.

- [ ] Write failing test: check the file does not yet exist before creating it.
- [ ] Create `migrations/data/nursery_seed.sql`:

  Required coverage (representative species per type — at minimum the stated examples):
  - **tree (乔木)**: 红枫 (*Acer palmatum* 'Atropurpureum'), 银杏 (*Ginkgo biloba*),
    广玉兰 (*Magnolia grandiflora*), 雪松 (*Cedrus deodara*), 水杉 (*Metasequoia
    glyptostroboides*), 法国梧桐 (*Platanus × acerifolia*), 国槐 (*Sophora japonica*),
    柳树 (*Salix babylonica*), 龙柏 (*Juniperus chinensis* 'Kaizuka'),
    白蜡 (*Fraxinus chinensis*), 栾树 (*Koelreuteria paniculata*), 樱花 (*Prunus
    serrulata*), 紫叶李 (*Prunus cerasifera* f. atropurpurea), 碧桃 (*Prunus
    persica* f. duplex), 合欢 (*Albizia julibrissin*), 五角枫 (*Acer mono*),
    黄连木 (*Pistacia chinensis*), 榆树 (*Ulmus pumila*), 皂荚 (*Gleditsia
    sinensis*), 朴树 (*Celtis sinensis*)  — 20+ tree entries
  - **shrub (灌木)**: 月季 (*Rosa chinensis*), 紫薇 (*Lagerstroemia indica*),
    连翘 (*Forsythia suspensa*), 迎春 (*Jasminum nudiflorum*), 石楠 (*Photinia
    serrulata*), 红叶石楠 (*Photinia × fraseri*), 大叶黄杨 (*Buxus megistophylla*),
    金叶女贞 (*Ligustrum × vicaryi*), 木槿 (*Hibiscus syriacus*), 海棠 (*Malus
    spectabilis*), 棣棠 (*Kerria japonica*), 榆叶梅 (*Prunus triloba*),
    紫荆 (*Cercis chinensis*), 八角金盘 (*Fatsia japonica*), 栀子花
    (*Gardenia jasminoides*), 金丝桃 (*Hypericum monogynum*), 夹竹桃 (*Nerium
    oleander*), 杜鹃 (*Rhododendron simsii*), 山茶 (*Camellia japonica*),
    茉莉 (*Jasminum sambac*) — 20+ shrub entries
  - **herb (地被/草本)**: 麦冬 (*Ophiopogon japonicus*), 鸢尾 (*Iris tectorum*),
    玉簪 (*Hosta plantaginea*), 萱草 (*Hemerocallis fulva*), 石竹 (*Dianthus
    chinensis*), 天竺葵 (*Pelargonium hortorum*), 一串红 (*Salvia splendens*),
    矮牵牛 (*Petunia hybrida*), 金盏菊 (*Calendula officinalis*), 孔雀草
    (*Tagetes patula*), 紫茉莉 (*Mirabilis jalapa*), 美人蕉 (*Canna indica*),
    羽衣甘蓝 (*Brassica oleracea* var. acephala), 芝樱 (*Phlox subulata*),
    波斯菊 (*Cosmos bipinnatus*), 醉鱼草 (*Buddleja davidii*) — 16+ herb entries
  - **vine (藤本)**: 紫藤 (*Wisteria sinensis*), 凌霄 (*Campsis grandiflora*),
    爬山虎 (*Parthenocissus tricuspidata*), 常春藤 (*Hedera helix*), 月光花
    (*Ipomoea alba*), 葫芦 (*Lagenaria siceraria*), 葡萄 (*Vitis vinifera*),
    木香 (*Rosa banksiae*), 金银花 (*Lonicera japonica*) — 9+ vine entries
  - **bamboo (竹类)**: 毛竹 (*Phyllostachys edulis*), 刚竹 (*Phyllostachys
    sulphurea*), 紫竹 (*Phyllostachys nigra*), 箬竹 (*Indocalamus tessellatus*),
    凤尾竹 (*Bambusa multiplex* 'Fernleaf'), 孝顺竹 (*Bambusa multiplex*),
    佛肚竹 (*Bambusa ventricosa*) — 7+ bamboo entries
  - **aquatic (水生)**: 睡莲 (*Nymphaea tetragona*), 荷花 (*Nelumbo nucifera*),
    鸢尾 (水生品种), 水葱 (*Schoenoplectus tabernaemontani*), 千屈菜
    (*Lythrum salicaria*), 水生美人蕉 (*Canna glauca*), 黄菖蒲 (*Iris
    pseudacorus*), 再力花 (*Thalia dealbata*), 梭鱼草 (*Pontederia cordata*),
    芦苇 (*Phragmites australis*) — 10+ aquatic entries
  - **bulb (球根)**: 百合 (*Lilium brownii*), 郁金香 (*Tulipa gesneriana*),
    水仙 (*Narcissus tazetta*), 风信子 (*Hyacinthus orientalis*), 大丽花
    (*Dahlia pinnata*), 唐菖蒲 (*Gladiolus × gandavensis*), 石蒜 (*Lycoris
    radiata*), 番红花 (*Crocus sativus*), 朱顶红 (*Hippeastrum vittatum*),
    晚香玉 (*Polianthes tuberosa*) — 10+ bulb entries
  - **fruit (果树)**: 苹果 (*Malus pumila*), 梨 (*Pyrus spp.*), 桃 (*Prunus
    persica*), 李 (*Prunus salicina*), 柿子 (*Diospyros kaki*), 核桃 (*Juglans
    regia*), 柑橘 (*Citrus reticulata*), 枇杷 (*Eriobotrya japonica*),
    石榴 (*Punica granatum*), 无花果 (*Ficus carica*), 葡萄 (*Vitis vinifera*,
    fruit type), 猕猴桃 (*Actinidia deliciosa*), 枣树 (*Ziziphus jujuba*),
    山楂 (*Crataegus pinnatifida*) — 14+ fruit entries

  Each row must supply a meaningful `spec_template` JSONB appropriate to the species type:
  - trees: `{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}`
  - shrubs: `{"冠幅_cm": null, "高度_cm": null, "枝条数": null}`
  - bamboo: `{"竿高_cm": null, "竿径_cm": null, "丛数": null}`
  - aquatic: `{"盆径_cm": null, "叶片数": null}`
  - bulbs: `{"球径_cm": null, "球重_g": null}`
  - fruits/vines: `{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}`

  Reasonable defaults for `best_season` (华东 / 华北 climate zones assumed):
  - Deciduous trees that transplant best in spring: `[3, 5]`
  - Evergreen trees: `[9, 11]`
  - Flowering shrubs: follows bloom cycle
  - Aquatic: `[4, 9]`
  - Bulbs: species-dependent;郁金香 `[9, 11]` (autumn planting), 百合 `[3, 4]`

  File header comment must cite data source:
  ```sql
  -- Nursery dictionary seed data — 200 common species
  -- Sources: 中国植物志 (Flora of China), public botanical databases
  -- Loaded when SEED_NURSERY_DICT=true at startup
  -- tenant_id = '00000000-0000-0000-0000-000000000000' = shared / public seed
  ```

- [ ] Create `internal/lifecycle/seed.go`:

  ```go
  // SeedNurseryDict inserts the nursery_seed.sql if SEED_NURSERY_DICT=true.
  // It is idempotent: rows with the seed tenant_id are skipped on conflict.
  func SeedNurseryDict(ctx context.Context, db DB, sqlPath string) error
  ```

  The function reads the SQL file path (passed from config) and executes it inside a transaction.
  Use `ON CONFLICT (tenant_id, name) DO NOTHING` in the INSERT statements to make execution
  idempotent.

- [ ] Wire `SeedNurseryDict` in `internal/lifecycle/start.go` after migrations, guarded by
  `os.Getenv("SEED_NURSERY_DICT") == "true"`. Log `"nursery seed: loaded N rows"` or
  `"nursery seed: skipped (SEED_NURSERY_DICT not set)"`.
- [ ] Add `SEED_NURSERY_DICT=false # set true to load 200 nursery species on startup` to
  `.env.example`.
- [ ] Verify: run `SEED_NURSERY_DICT=true make dev` locally; `GET /api/v1/nursery-dict?limit=200`
  returns `total >= 200`.

---

### Task 8: Frontend — API client `web/lib/api/nursery-dict.ts` (0.5h)

- [ ] Write failing test: `web/lib/api/nursery-dict.test.ts` (vitest + `fetch` mocked via
  `vi.stubGlobal`):
  - `TestNurseryDictApi_List_ParsesPaginatedResponse` — stub fetch returning
    `{ items: [{id:"1",name:"红枫",...}], total: 1 }`; call `listNurseryDict({})`; assert
    `result.items[0].name === "红枫"`.
  - `TestNurseryDictApi_Create_Returns201Data` — stub fetch returning a created item;
    call `createNurseryDict({name:"测试",...})`; assert returned item has the correct `name`.
  - `TestNurseryDictApi_Delete_CallsDeleteEndpoint` — stub fetch; call `deleteNurseryDict("id1")`;
    assert fetch was called with method `"DELETE"` and URL containing `"id1"`.
  - `TestNurseryDictApi_Restore_CallsRestoreEndpoint` — stub fetch; call
    `restoreNurseryDict("id1")`; assert fetch called with method `"POST"` and URL containing
    `"restore"`.
- [ ] Create `web/lib/api/nursery-dict.ts`:

  ```typescript
  export interface NurseryDictItem {
    id: string
    tenantId: string
    name: string
    latinName: string
    family: string
    genus: string
    type: NurseryType
    isEvergreen: boolean
    climateZones: string[]
    bestSeason: [number, number]
    specTemplate: Record<string, unknown>
    defaultUnitId?: string
    photoUrl: string
    remark: string
    createdAt: string
    updatedAt: string
  }

  export type NurseryType =
    | 'tree' | 'shrub' | 'herb' | 'vine' | 'bamboo'
    | 'aquatic' | 'bulb' | 'fruit'

  export interface NurseryDictListParams {
    q?: string
    type?: NurseryType
    isEvergreen?: boolean
    limit?: number
    offset?: number
  }

  export interface NurseryDictListResult {
    items: NurseryDictItem[]
    total: number
  }

  export type NurseryDictCreateInput = Omit<
    NurseryDictItem, 'id' | 'tenantId' | 'createdAt' | 'updatedAt'
  >

  export function listNurseryDict(params: NurseryDictListParams): Promise<NurseryDictListResult>
  export function getNurseryDict(id: string): Promise<NurseryDictItem>
  export function createNurseryDict(input: NurseryDictCreateInput): Promise<NurseryDictItem>
  export function updateNurseryDict(id: string, input: Partial<NurseryDictCreateInput>): Promise<NurseryDictItem>
  export function deleteNurseryDict(id: string): Promise<void>
  export function restoreNurseryDict(id: string): Promise<NurseryDictItem>
  ```

  Follows the same `fetch` + auth header pattern used in `web/lib/api/products.ts`.

- [ ] Verify: `bun run test -- lib/api/nursery-dict` passes.

---

### Task 9: Frontend — list page `web/app/(dashboard)/dictionary/page.tsx` (2h)

- [ ] Write failing test: `web/app/(dashboard)/dictionary/page.test.tsx` (vitest +
  `@testing-library/react`):
  - `TestDictionaryPage_Renders_SearchInputAndTable` — mock `listNurseryDict` returning
    `{ items: [], total: 0 }`; render page; assert search input is present; assert table
    headers include "Name" / "Latin Name" / "Type".
  - `TestDictionaryPage_ListItems_RenderedInTable` — mock returns 3 items; render; assert 3
    rows visible.
  - `TestDictionaryPage_Search_CallsApiWithQuery` — type "红枫" in search input; assert
    `listNurseryDict` was called with `{ q: "红枫" }`.
  - `TestDictionaryPage_AddButton_IsPresent` — assert button text "新增苗木" is present.
- [ ] Create `web/app/(dashboard)/dictionary/page.tsx` (client component):
  - Top bar: search `<Input>` (debounced 300ms via `useDebounce` or `setTimeout`), type filter
    `<Select>` (all types or a specific `NurseryType`), "+ 新增苗木" `<Button>`.
  - Table using existing shadcn `<Table>` convention; columns:
    `名称 | 拉丁名 | 科 | 类型 | 落叶/常绿 | 默认单位 | 操作`.
  - Clicking a row opens the detail drawer (Task 10).
  - Pagination: "上一页 / 下一页" buttons with `offset` state; page size 20.
  - Empty state: "暂无苗木，点击'新增苗木'添加第一个品种".
  - Uses `listNurseryDict` from the API client; React Query or plain `useEffect` — follow the
    pattern used in `web/app/(dashboard)/products/page.tsx`.
- [ ] Verify: `bun run test -- dictionary/page` passes; `bun run build` exits 0.

---

### Task 10: Frontend — detail drawer + create/edit form (2h)

- [ ] Write failing test: `web/components/horticulture/NurseryDictForm.test.tsx` (vitest +
  `@testing-library/react`):
  - `TestNurseryDictForm_RequiredFields_ShowsValidationError` — submit with empty `name`;
    assert validation error message is visible.
  - `TestNurseryDictForm_SubmitCreate_CallsCreateApi` — fill name + type; submit; assert
    `createNurseryDict` was called once with the correct payload.
  - `TestNurseryDictForm_SubmitEdit_CallsUpdateApi` — render in edit mode with `initialData`;
    change `remark`; submit; assert `updateNurseryDict` was called.
  - `TestNurseryDictForm_SpecTemplate_RenderedAsKeyValueInputs` — pass
    `specTemplate={"胸径_cm":null}`; assert input labeled "胸径_cm" is visible.
- [ ] Create `web/components/horticulture/NurseryDictForm.tsx`:
  - Fields: 名称 (required), 拉丁名, 科, 属, 类型 (select enum), 落叶/常绿 (checkbox),
    气候带 (multi-tag input — plain comma-separated text for MVP), 最佳移植期起止月份
    (two `<Select>` month pickers), 规格模板 (dynamic key-value editor where each key is
    a text input and value is always null/empty — "+ 添加规格项" button appends a row),
    默认单位 (select from existing unit_def list), 图片URL (text input), 备注 (textarea).
  - Submit calls `createNurseryDict` (new) or `updateNurseryDict` (edit).
  - Emits `onSuccess(item: NurseryDictItem)` prop on successful save.
- [ ] Create `web/app/(dashboard)/dictionary/[id]/page.tsx` — OR implement as a side drawer
  on the list page using shadcn `<Sheet>`:
  - Shows all fields read-only when `mode="view"`.
  - "编辑" button switches to `<NurseryDictForm>` in edit mode.
  - "软删除" button calls `deleteNurseryDict(id)` + toast "已删除，可按 Cmd+Z 恢复".
  - "恢复" button (visible only for deleted items) calls `restoreNurseryDict(id)`.
  - **Note**: This story uses a Sheet drawer on the list page (not a separate `/[id]/page.tsx`)
    for simplicity. A `[id]/page.tsx` deep-link page is a follow-up (out of scope).
- [ ] Add "苗木字典" to sidebar navigation in `web/app/(dashboard)/sidebar.tsx`:
  - Icon: `<Leaf />` from `lucide-react` (already a dependency).
  - Label: "苗木字典".
  - Route: `/dictionary`.
  - No profile gate for now (profile gating is Story 28.4 scope per PRD §5).
- [ ] Verify: `bun run test -- components/horticulture` passes; `bun run build` exits 0;
  `bun run lint` exits 0.

---

### Task 11: Playwright E2E — `web/tests/e2e/nursery-dict.spec.ts` (1h)

- [ ] Write the E2E spec:

  ```typescript
  // web/tests/e2e/nursery-dict.spec.ts
  // Requires: seed data loaded (SEED_NURSERY_DICT=true on the test backend)
  // Run: bunx playwright test nursery-dict.spec.ts

  test('dictionary list page shows 200 seed species', async ({ page }) => {
    await page.goto('/dictionary')
    // Wait for table rows to render
    const rows = page.locator('tbody tr')
    await expect(rows).toHaveCount(20)  // default page size
    // Check total count label shows >= 200
    await expect(page.locator('[data-testid="total-count"]')).toContainText('200')
  })

  test('search for 红枫 returns at least 1 result', async ({ page }) => {
    await page.goto('/dictionary')
    await page.fill('[placeholder*="搜索"]', '红枫')
    await page.waitForTimeout(400)  // debounce
    const rows = page.locator('tbody tr')
    await expect(rows).toHaveCountGreaterThan(0)
    await expect(rows.first()).toContainText('红枫')
  })

  test('click row opens drawer with latin name', async ({ page }) => {
    await page.goto('/dictionary')
    await page.locator('tbody tr').first().click()
    await expect(page.locator('[data-testid="nursery-detail-drawer"]')).toBeVisible()
    await expect(page.locator('[data-testid="latin-name"]')).not.toBeEmpty()
  })

  test('create new entry and it appears in list', async ({ page }) => {
    await page.goto('/dictionary')
    await page.click('button:has-text("新增苗木")')
    await page.fill('[name="name"]', 'E2E测试苗木')
    await page.selectOption('[name="type"]', 'shrub')
    await page.click('button[type="submit"]')
    await expect(page.locator('tbody')).toContainText('E2E测试苗木')
  })
  ```

- [ ] Verify spec file exists and is syntactically valid (`bunx tsc --noEmit` passes on it).

---

## File List

### New files (create)

| Path | Notes |
|------|-------|
| `migrations/000028_nursery_dict.up.sql` | Table DDL + RLS |
| `migrations/000028_nursery_dict.down.sql` | DROP TABLE + DROP TYPE |
| `migrations/data/nursery_seed.sql` | 200 species INSERT statements |
| `internal/domain/horticulture/dict.go` | Domain entity + NurseryType enum + CreateInput + UpdateInput + ListFilter |
| `internal/domain/horticulture/dict_test.go` | Domain validation unit tests |
| `internal/app/horticulture/repository.go` | Repository interface + ErrNotFound + ErrDuplicateName |
| `internal/app/horticulture/create.go` | CreateUseCase |
| `internal/app/horticulture/get.go` | GetByIDUseCase |
| `internal/app/horticulture/list.go` | ListUseCase |
| `internal/app/horticulture/update.go` | UpdateUseCase |
| `internal/app/horticulture/delete.go` | DeleteUseCase |
| `internal/app/horticulture/restore.go` | RestoreUseCase |
| `internal/app/horticulture/dict_usecases_test.go` | App layer unit tests (fake repo) |
| `internal/adapter/repo/horticulture/dict_repo.go` | SQL-based repository implementation |
| `internal/adapter/repo/horticulture/dict_repo_test.go` | Integration tests (tagged) |
| `internal/adapter/handler/horticulture/dict_handler.go` | Gin handler + DTO types |
| `internal/adapter/handler/horticulture/dict_handler_test.go` | Handler unit tests (httptest) |
| `internal/lifecycle/seed.go` | SeedNurseryDict function |
| `web/lib/api/nursery-dict.ts` | Fetch-based API client |
| `web/lib/api/nursery-dict.test.ts` | Vitest unit tests for API client |
| `web/components/horticulture/NurseryDictForm.tsx` | New/edit form component |
| `web/components/horticulture/NurseryDictForm.test.tsx` | Vitest component tests |
| `web/app/(dashboard)/dictionary/page.tsx` | List page with search + table |
| `web/app/(dashboard)/dictionary/page.test.tsx` | Vitest page tests |
| `web/tests/e2e/nursery-dict.spec.ts` | Playwright E2E spec |

### Modified files

| Path | What changes |
|------|-------------|
| `internal/adapter/handler/router/router.go` | Add `*handlerhorticulture.DictHandler` param; register routes; add nil-guard fallback |
| `internal/adapter/handler/router/router_test.go` | Assert nursery-dict routes are registered |
| `internal/lifecycle/app.go` | Construct horticulture DI chain; pass `dictHandler` to router |
| `internal/lifecycle/start.go` | Call `SeedNurseryDict` after migrations when env var set |
| `web/app/(dashboard)/sidebar.tsx` | Add "苗木字典" nav item with Leaf icon |
| `.env.example` | Add `SEED_NURSERY_DICT=false` with comment |

---

## Test Plan

### Go unit tests (`go test ./... -v -race`)

| Package | Key test functions |
|---------|-------------------|
| `internal/domain/horticulture` | `TestNurseryDict_Validate_RejectsEmptyName`, `TestNurseryDict_Validate_RejectsBadSeason`, `TestNurseryType_String_AllValuesRoundtrip` |
| `internal/app/horticulture` | `TestCreateUseCase_Execute_ReturnsDuplicateNameError`, `TestCreateUseCase_Execute_HappyPath`, `TestDeleteUseCase_Execute_SetsDeletedAt`, `TestDeleteUseCase_Execute_NotFoundError`, `TestRestoreUseCase_Execute_HappyPath`, `TestListUseCase_Execute_DefaultsLimit` |
| `internal/adapter/handler/horticulture` | `TestDictHandler_List_Returns200WithItems`, `TestDictHandler_Create_Returns201`, `TestDictHandler_Create_DuplicateName_Returns409`, `TestDictHandler_GetByID_Returns404ForUnknown`, `TestDictHandler_Update_Returns200`, `TestDictHandler_Delete_Returns204`, `TestDictHandler_Restore_Returns200` |
| `internal/adapter/handler/router` | `TestRouter_RegistersNurseryDictRoutes` |

### Go integration tests (`go test -tags integration ./...`)

| Package | Key test functions |
|---------|-------------------|
| `internal/adapter/repo/horticulture` | `TestDictRepo_Create_HappyPath`, `TestDictRepo_Create_DuplicateNameReturnsError`, `TestDictRepo_List_FiltersOnQuery`, `TestDictRepo_Delete_SoftDeletesRow`, `TestDictRepo_Restore_MakesRowVisibleAgain`, `TestDictRepo_RLS_TenantBCannotSeeTenantARow`, `TestDictRepo_SeedRows_VisibleToAllTenants` |

Target coverage: app layer ≥ 80%, repo ≥ 60%, handler ≥ 50% (matching project TDD targets).

### Vitest frontend tests (`bun run test`)

| File | Key scenarios |
|------|--------------|
| `web/lib/api/nursery-dict.test.ts` | List parses paginated response; create returns 201 data; delete calls DELETE; restore calls POST restore |
| `web/components/horticulture/NurseryDictForm.test.tsx` | Required fields validation; submit create calls API; submit edit calls update API; spec_template rendered as key-value inputs |
| `web/app/(dashboard)/dictionary/page.test.tsx` | Renders search + table; items in table; search calls API with query; add button present |

### Playwright E2E (`bunx playwright test nursery-dict.spec.ts`)

| Spec | Scenario |
|------|---------|
| `nursery-dict.spec.ts` | List page shows 200 seed species (total count label ≥ 200) |
| `nursery-dict.spec.ts` | Search "红枫" returns ≥ 1 result with "红枫" in row |
| `nursery-dict.spec.ts` | Click row → drawer opens → latin_name not empty |
| `nursery-dict.spec.ts` | Create new entry → appears in list |

E2E requires `SEED_NURSERY_DICT=true` on the test backend and a logged-in session (use
`auth.setup.ts` that already exists in `web/tests/e2e/`).

---

## Dev Notes

### Migration numbering

Existing migration head is `000027`. This story creates `000028`. Before writing the file,
run `ls migrations/ | tail -3` to confirm the current head. If any migration between 000027
and 000028 was added by another story in flight, use the next available number and update this
story's file references accordingly. Per `doc/coord/migration-ledger.md` protocol, reserve the
ID before writing code.

### `pg_trgm` and the GIN trigram index

`000001_init_extensions.up.sql` must enable `pg_trgm` before migration 000028 is applied.
Verify with `SELECT * FROM pg_extension WHERE extname='pg_trgm'`. If absent, add
`CREATE EXTENSION IF NOT EXISTS pg_trgm;` to migration 000028 itself (idempotent). Without
`pg_trgm`, the `gin_trgm_ops` index will fail with "operator class does not exist". Fallback:
replace that index with a plain B-tree on `name` — slower substring search but functionally
correct.

### Shared-seed RLS pattern (HD-8)

The shared seed uses `tenant_id = '00000000-0000-0000-0000-000000000000'` (the nil UUID). The
RLS policy must include an OR clause to expose these rows to all authenticated tenants. This is
the same pattern used for system-wide reference data. The nil UUID literal is safe because it
is not a valid Zitadel user ID format. Do NOT use `tenant_id = NULL` — NULL comparisons in RLS
silently exclude rows.

### `nursery_type` ENUM vs CHECK constraint

The migration uses a native PostgreSQL ENUM type (`tally.nursery_type`). The `.down.sql` must
drop the ENUM after dropping the table. If the ENUM is shared with other tables in a future
migration, adjust the down SQL. For now the ENUM is used only by `nursery_dict`. Alternative:
use `VARCHAR(20) CHECK (type IN (...))` — avoids the ENUM lifecycle complexity but loses
DB-level type safety. The ENUM approach is consistent with the architecture doc's strict typing
approach for categorical fields.

### Go package naming

Create a new package `horticulture` under each layer:
- `internal/domain/horticulture` — package `horticulture`
- `internal/app/horticulture` — package `horticulture`
- `internal/adapter/repo/horticulture` — package `horticulture`
- `internal/adapter/handler/horticulture` — package `horticulture`

When both app and domain packages are named `horticulture`, the repo and handler must use an
import alias to distinguish them:
```go
import (
    domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
    apphort "github.com/hanmahong5-arch/lurus-tally/internal/app/horticulture"
)
```
Follow the same aliasing convention already used in `internal/adapter/repo/product/repo.go`.

### `router.New(...)` signature change

Adding `*handlerhorticulture.DictHandler` as a new parameter to `router.New` is a breaking
change to all call sites. `internal/lifecycle/app.go` is the only real call site; test files
that call `router.New(nil, nil, ...)` must be updated to pass a nil `*handlerhorticulture.DictHandler`
as the new parameter. Read `internal/adapter/handler/router/router_test.go` before modifying
the signature to count the exact call sites.

### Seed file idempotency

All INSERT statements in `nursery_seed.sql` must use `ON CONFLICT (tenant_id, name) DO NOTHING`.
This ensures re-running `SEED_NURSERY_DICT=true` on a system that already has seed data is a
no-op (no duplicate key errors, no data changes). The seed runner (`SeedNurseryDict`) should
log how many rows were actually inserted (`result.RowsAffected()`) so operators can verify.

### Seed data — botanical accuracy

The species names and Latin binomials in the seed file should be drawn from verifiable public
sources (中国植物志 online, POWO, or similar). The dev agent is responsible for cross-checking
that Latin names are correctly formatted (Genus species Author) and that the family assignments
are consistent with APG IV classification. If a species name cannot be verified, mark the row
with `remark = 'latin_name_unverified'`. The goal is practical utility (client can search "红枫"
and get a result), not academic precision.

### Frontend: `[id]/page.tsx` vs sheet drawer

This story implements the detail view as a `<Sheet>` drawer on the list page (not a separate
route) to match the UI/UX decision in the horticulture PRD §7 ("苗木字典: 行点抽屉详情"). A
dedicated deep-link `/dictionary/[id]` route is deferred to Story 28.13 when the full detail
page (price history curve + related projects) is built.

### `sidebar.tsx` modification risk

The sidebar file already exists at `web/app/(dashboard)/sidebar.tsx`. Read it before modifying
to understand the existing nav item structure. Add the new item adjacent to the "商品" (products)
group — horticulture is a product-adjacent feature. Do not remove or reorder existing items.

### `BestSeason` zero value

The domain `BestSeason [2]int` uses `[0, 0]` as the "unset" sentinel (Go zero value for
`[2]int`). When storing in PostgreSQL, `[0, 0]` maps to `INT[] = '{0, 0}'`. The repo layer
should treat `[0, 0]` as "not set" and store `'{}'` (empty INT[]) instead. Validate in
`NurseryDict.Validate()` that months are either both 0 (unset) or both in range [1, 12].

---

## Definition of Done

- [ ] `go test ./internal/domain/horticulture/... ./internal/app/horticulture/... ./internal/adapter/handler/horticulture/... -v -count=1 -race` exits 0.
- [ ] `go test -tags integration ./internal/adapter/repo/horticulture/... -v` exits 0 (requires `TEST_DSN`).
- [ ] `CGO_ENABLED=0 GOOS=linux go build ./...` exits 0.
- [ ] `golangci-lint run ./...` exits 0.
- [ ] `cd web && bun run test` exits 0 (new tests pass; pre-existing failures unchanged).
- [ ] `cd web && bun run build` exits 0.
- [ ] `cd web && bun run lint` exits 0.
- [ ] Migration 000028 applies cleanly: `make migrate-up` exits 0; `make migrate-down` rolls it back cleanly.
- [ ] `SEED_NURSERY_DICT=true` at startup loads ≥ 200 rows; re-running is idempotent (0 rows inserted on second run due to `ON CONFLICT DO NOTHING`).
- [ ] `GET /api/v1/nursery-dict?q=红枫` returns ≥ 8 results after seed is loaded.
- [ ] AC-3 verified: POST duplicate name returns 409.
- [ ] AC-4 verified: soft-delete hides row; restore brings it back (manual curl or Playwright).
- [ ] AC-7 verified: tenant isolation confirmed by repo integration test `TestDictRepo_RLS_TenantBCannotSeeTenantARow`.
- [ ] Playwright E2E spec `nursery-dict.spec.ts` written (passing against stage with seed data loaded is ideal; failing against offline environment is acceptable — mark skipped).
- [ ] `doc/coord/service-status.md` updated: Tally block story 28.1 → Done.
- [ ] `doc/process.md` updated with ≤15-line summary.

---

## Dependencies

| # | Dependency | Relationship |
|---|-----------|-------------|
| D1 | Migration 000028 must be applied before any code that queries `tally.nursery_dict` | Hard: deploy migration before rolling out the backend image that registers the routes |
| D2 | `tally.unit_def` table (migration 000014) must exist | Hard: `nursery_dict.default_unit_id` references `tally.unit_def(id)`; migration 000014 must be at head |
| D3 | `pg_trgm` extension must be enabled | Hard for trigram index; soft for functionality (fallback B-tree index works) |
| D4 | Stories 28.2+ (project CRUD) | 28.2 depends on 28.1 being done; no reverse dependency |
| D5 | Story 28.12 (dedicated seed refinement) | 28.12 expands / corrects the initial 200-entry seed; 28.1 seed is the baseline |
| D6 | Story 28.13 (drawer detail with price history) | 28.13 requires the `NurseryDictItem` type from this story's API client |

---

## Risks and Assumptions

| # | Item | Risk if wrong | Resolution |
|---|------|--------------|------------|
| A1 | Migration numbering: next available is 000028 | If another story created a 000028 migration concurrently, this story's file name conflicts | Dev agent: check `ls migrations/ | sort | tail -1` before creating the file; adjust number if needed |
| A2 | `pg_trgm` is enabled in migration 000001 | If absent, `gin_trgm_ops` index creation fails with DDL error | Dev agent: inspect `000001_init_extensions.up.sql` before writing the migration; add `CREATE EXTENSION IF NOT EXISTS pg_trgm` in 000028 if missing |
| A3 | `internal/lifecycle/app.go` passes arguments positionally to `router.New` | Adding a new parameter in the middle of the signature breaks positional call sites | Dev agent: read the full `router.New` signature and all call sites before modifying |
| A4 | The 200 seed species names and Latin binomials are accurate enough for client use | Client complains about incorrect Latin names in a customer-facing context | Marked "latin_name_unverified" for entries where the dev agent cannot verify; customer can edit via the CRUD UI |
| A5 | `web/app/(dashboard)/sidebar.tsx` uses a specific nav item structure (e.g., grouped by section) | Incorrect insertion breaks existing nav items visually | Dev agent: read the full sidebar.tsx before modifying; match the existing item JSX structure exactly |
| A6 | Seed data is safe in automated tests | If a test suite runs `SEED_NURSERY_DICT=true` on a shared Postgres, it pollutes other tenants' queries | Seed rows use `tenant_id=nil UUID`; they appear in all tenant queries. Tests that assert exact counts (e.g., `total == 0`) will break if seed is loaded. The default is `SEED_NURSERY_DICT=false`; CI must not set this env var |
| A7 | `BestSeason [2]int` serializes cleanly to PostgreSQL `INT[]` via `database/sql` | Driver may not auto-convert `[2]int` to `INT[]` — may need manual `pq.Array` wrapper | Dev agent: check how existing `[]string` fields (e.g., `climate_zones TEXT[]`) are stored in the product repo; apply the same wrapper technique |
| A8 | `lucide-react` already includes a `<Leaf />` icon component | If the icon is absent, the sidebar item import fails | Dev agent: run `bunx tsc --noEmit` after adding the import; if `Leaf` is absent, use `<TreePine />` or `<Sprout />` as alternatives |

---

## Dev Agent Record

**Status**: Done — 2026-04-30

**Pre-flight findings**:
- A1: Migration head confirmed at 000027; created 000028 with no collision.
- A2: `pg_trgm` IS enabled in migration 000001 — `gin_trgm_ops` index safe to use.
- A3: `router.New` has 12 params; `router_test.go` `newTestRouter()` passes 12 nils. Added 13th param `dh *handlerhorticulture.DictHandler` and updated all call sites (app.go + router_test.go).
- A6: Seed gated by `SEED_NURSERY_DICT=true` env var — implemented in `lifecycle/seed.go` and `start.go`. Default OFF.
- A7: Product repo uses `sliceToArray` (manual string conversion) for TEXT[], NOT `pq.Array`. Applied same pattern for `best_season INT[]` via `intSliceToArray`. Zero-valued `[0,0]` stored as `{}`.
- A8: `Leaf` icon IS available in `lucide-react`. Used emoji `🌿` in sidebar instead (matches existing sidebar style which uses emoji strings, not lucide components).

**Decisions**:
- Sidebar uses emoji `🌿` for "苗木字典" to match the existing `NavItem.icon: string` pattern (sidebar uses emoji strings, not React components).
- `restoreNurseryDict` wired in drawer view as a "恢复" button even without deleted state guard (UI simplification for MVP; state check deferred to Story 28.13).
- Handler test uses `fakeHandlerRepo` (not a mock framework) per project convention.
- Page test seeds 2 items to render table headers (table only renders when `items.length > 0`).
- E2E spec uses `test.skip` for seed-dependent tests; 2 non-skip smoke tests (header + button) can run without seed.

**Deviations**:
- None from the architecture decisions. Sidebar `lucide-react` import skipped per pattern match with existing code.
- `TestDictionaryPage_Renders_SearchInputAndTable` uses 2 mock items instead of empty list to exercise table header rendering.

**Test results**:
- Go: 18 tests pass across 5 packages (domain/horticulture, app/horticulture, adapter/handler/horticulture, adapter/repo/horticulture, adapter/handler/router).
- Frontend: 182 tests pass (+13 new); 3 pre-existing auth-session failures unchanged; 9 e2e Playwright specs fail under vitest (pre-existing pattern).
- `CGO_ENABLED=0 GOOS=linux go build ./...` exits 0.
- `bun run build` exits 0.
- `bun run lint` exits 0 (1 pre-existing Palette.tsx warning).
- `bunx tsc --noEmit` exits 0.
- `gofmt -l ./internal` returns empty.
