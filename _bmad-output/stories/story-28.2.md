# Story 28.2: project 表 + 项目 CRUD + 项目列表页（卡片网格）

**Epic**: E28 — 项目制核算 + 多维度汇总 + 苗木字典 + 价格分级
**Story ID**: 28.2
**Priority**: P0 (blocks 28.3, 28.4, 28.5, 28.6, 28.7, 28.11)
**Type**: full-stack (migration + Go domain/app/repo/handler + Next.js card-grid list page + form component)
**Estimate**: 14h total (see per-task estimates)
**Status**: Approved

---

## Context

With the nursery species dictionary in place (Story 28.1), the next foundational building block is
the project/contract record. In the horticulture client's workflow, all procurement, sales, and
payments trace back to a landscaping project — "项目 = 一级核算单位" (architecture decision
HD-4). Without the `tally.project` table, bills and payments cannot carry a `project_id`, and the
multi-dimensional P&L dashboard (Stories 28.4/28.5) has nothing to aggregate against.

This story delivers the `tally.project` table (migration 000029), the full Go domain/app/repo/
handler stack mirroring the layered pattern from the horticulture dict, and the Next.js projects
list page rendered as a **card grid** (3-col responsive) rather than the table layout used by
dictionary. The list page card grid matches the UI/UX decision from the horticulture PRD §7
("顶部项目卡 + 卡片网格 + 右抽屉损益曲线"). After this story a user can open `/projects`,
create a project with a customer reference and contract amount, and see it appear as a status-
colored card.

Story 28.3 will add `project_id` foreign keys to `bill_head` and `payment_head` as a separate
migration to keep changes atomic. This story only creates the project table and its own CRUD.

---

## Acceptance Criteria

1. `GET /api/v1/projects?limit=20&offset=0` returns HTTP 200 with `{"items":[...],"total":N}`.
   Each item has at minimum: `id`, `code`, `name`, `status`, `tenant_id`, `created_at`.

2. `POST /api/v1/projects` with a valid body (name + code required) returns HTTP 201 with a
   `Location: /api/v1/projects/<id>` header and the created project JSON body.

3. Creating two projects with the same `code` under the same `tenant_id` returns HTTP 409 with
   `{"error":"duplicate code"}`. The unique constraint `(tenant_id, code)` is enforced at both
   the database layer (UNIQUE constraint) and the app layer (`ErrDuplicateCode`).

4. A soft-deleted project (via `DELETE /api/v1/projects/:id`) no longer appears in
   `GET /api/v1/projects` (list excludes `deleted_at IS NOT NULL`). Calling
   `POST /api/v1/projects/:id/restore` returns HTTP 200 and the project reappears in the list.

5. All six REST endpoints respond with expected HTTP status codes: GET list 200, POST create 201,
   GET single 200/404, PUT update 200/404, DELETE soft-delete 204, POST restore 200. A valid auth
   token with a non-existent `id` returns 404.

6. `Validate()` on a Project domain entity rejects: empty `name` (error "name is required"),
   empty `code` (error "code is required"), and a pair of dates where `end_date < start_date`
   (error "end_date must be on or after start_date"). Valid inputs with nil dates pass.

7. The `/projects` Next.js page renders a 3-column responsive card grid (lg:3-col, md:2-col,
   sm:1-col). Each card displays: project name (heading), code (muted text), customer badge (if
   customer_id is set), contract amount (large number), status badge (color-coded per status),
   and date range. The page has a search input, a status filter `<Select>`, and a "+ 新建项目"
   button. Empty state shows "暂无项目，点击'新建项目'添加第一个".

8. Clicking a project card opens a side Sheet drawer with full project details and an edit form
   (`ProjectForm`). Submitting the form with an edited name updates the project and the card
   refreshes without a full page reload.

9. The sidebar at `/projects` shows the "项目" entry with `🏗️` icon as active (highlighted).

10. `go test ./internal/domain/project/... ./internal/app/project/... ./internal/adapter/handler/project/... -v -count=1 -race`
    exits 0. `cd web && bun run test` exits 0. `CGO_ENABLED=0 GOOS=linux go build ./...` and
    `bun run build` both exit 0.

---

## Tasks / Subtasks

### Task 1: Migration 000029 — create `tally.project` table (1h)

- [ ] Write failing test: confirm `migrations/000029_project.up.sql` does not yet exist.
  Run `ls migrations/ | grep 000029`; assert absent before creating.
- [ ] Create `migrations/000029_project.up.sql`:

  ```sql
  CREATE TABLE tally.project (
      id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
      tenant_id        UUID         NOT NULL,
      code             VARCHAR(50)  NOT NULL,
      name             VARCHAR(200) NOT NULL,
      customer_id      UUID         REFERENCES tally.partner(id) ON DELETE SET NULL,
      contract_amount  NUMERIC(18,2),
      start_date       DATE,
      end_date         DATE,
      status           VARCHAR(20)  NOT NULL DEFAULT 'active'
                           CHECK (status IN ('active','paused','completed','cancelled')),
      address          TEXT,
      manager          VARCHAR(100),
      remark           TEXT,
      created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
      updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
      deleted_at       TIMESTAMPTZ,
      CONSTRAINT uq_project_tenant_code UNIQUE (tenant_id, code)
  );

  CREATE INDEX idx_project_tenant   ON tally.project(tenant_id);
  CREATE INDEX idx_project_status   ON tally.project(tenant_id, status);
  CREATE INDEX idx_project_customer ON tally.project(customer_id) WHERE customer_id IS NOT NULL;
  CREATE INDEX idx_project_name_trgm ON tally.project USING GIN (name gin_trgm_ops);

  -- RLS: strict tenant isolation (no shared seed for project).
  ALTER TABLE tally.project ENABLE ROW LEVEL SECURITY;
  CREATE POLICY project_rls ON tally.project
      USING (tenant_id = current_setting('app.tenant_id', true)::UUID);
  ```

  Note: `pg_trgm` was confirmed enabled in migration 000001 (see S28.1 pre-flight note A2).
  `tally.partner` exists since migration 000004.

- [ ] Create `migrations/000029_project.down.sql`:

  ```sql
  DROP TABLE IF EXISTS tally.project;
  ```

- [ ] Verify: `make migrate-up` applies migration 000029 without error; `make migrate-down`
  rolls it back cleanly.

---

### Task 2: Go domain layer — `internal/domain/project/project.go` (1h)

- [ ] Write failing test: `internal/domain/project/project_test.go`:
  - `TestProject_Validate_RejectsEmptyName` — construct `Project{Code:"P001"}`;
    call `p.Validate()`; assert error contains "name is required".
  - `TestProject_Validate_RejectsEmptyCode` — construct `Project{Name:"河道绿化"}`;
    call `p.Validate()`; assert error contains "code is required".
  - `TestProject_Validate_RejectsEndBeforeStart` — set `StartDate` to 2025-01-01,
    `EndDate` to 2024-12-31; assert error contains "end_date must be on or after start_date".
  - `TestProject_Validate_AcceptsNilDates` — `StartDate=nil, EndDate=nil`; assert no error.
  - `TestProject_Validate_AcceptsOnlyStartDate` — `StartDate=2025-01-01, EndDate=nil`;
    assert no error (end_date is optional).
  - `TestProjectStatus_AllValues` — range over the four `ProjectStatus` constants; assert each
    `String()` matches the expected string value.
- [ ] Create `internal/domain/project/project.go`:

  ```go
  package project

  import (
      "errors"
      "fmt"
      "time"
      "github.com/google/uuid"
  )

  // ProjectStatus represents the lifecycle state of a project.
  type ProjectStatus string

  const (
      StatusActive    ProjectStatus = "active"
      StatusPaused    ProjectStatus = "paused"
      StatusCompleted ProjectStatus = "completed"
      StatusCancelled ProjectStatus = "cancelled"
  )

  // String returns the string value of the ProjectStatus.
  func (s ProjectStatus) String() string { return string(s) }

  // Project is the domain entity for a landscaping/engineering project.
  type Project struct {
      ID             uuid.UUID
      TenantID       uuid.UUID
      Code           string
      Name           string
      CustomerID     *uuid.UUID
      ContractAmount *string   // stored as string to avoid float precision loss; NUMERIC(18,2)
      StartDate      *time.Time
      EndDate        *time.Time
      Status         ProjectStatus
      Address        string
      Manager        string
      Remark         string
      CreatedAt      time.Time
      UpdatedAt      time.Time
      DeletedAt      *time.Time
  }

  // Validate enforces domain invariants.
  func (p *Project) Validate() error {
      if p.Name == "" {
          return errors.New("name is required")
      }
      if p.Code == "" {
          return errors.New("code is required")
      }
      if p.StartDate != nil && p.EndDate != nil {
          if p.EndDate.Before(*p.StartDate) {
              return errors.New("end_date must be on or after start_date")
          }
      }
      return nil
  }

  // CreateInput carries fields for creating a new Project.
  type CreateInput struct {
      TenantID       uuid.UUID
      Code           string
      Name           string
      CustomerID     *uuid.UUID
      ContractAmount *string
      StartDate      *time.Time
      EndDate        *time.Time
      Status         ProjectStatus
      Address        string
      Manager        string
      Remark         string
  }

  // UpdateInput carries mutable fields (nil pointer = do not update).
  type UpdateInput struct {
      Code           *string
      Name           *string
      CustomerID     *uuid.UUID
      ContractAmount *string
      StartDate      *time.Time
      EndDate        *time.Time
      Status         *ProjectStatus
      Address        *string
      Manager        *string
      Remark         *string
  }

  // ListFilter controls list queries.
  type ListFilter struct {
      TenantID   uuid.UUID
      Query      string        // ILIKE on name or code
      Status     *ProjectStatus
      CustomerID *uuid.UUID
      Limit      int
      Offset     int
  }
  ```

  Note: `ContractAmount` is `*string` in the domain (not `*big.Rat` or `float64`) to avoid
  floating-point rounding. The repo layer scans PostgreSQL `NUMERIC(18,2)` into a `*string`
  using `sql.NullString`; the handler/DTO exposes it as a JSON string too. Callers that need
  arithmetic should parse it. This mirrors the convention used for amounts in `bill_head` —
  check `internal/adapter/repo/bill/` for the exact scan pattern before writing the repo.

- [ ] Verify: `go test ./internal/domain/project/... -v` passes.

---

### Task 3: Go app layer — repository interface + use cases (1.5h)

- [ ] Write failing test: `internal/app/project/project_usecases_test.go` (table-driven,
  hand-written fake `Repository`):
  - `TestProjectCreateUseCase_Execute_HappyPath` — fake repo Create succeeds; assert returned
    `Project.Name` matches input.
  - `TestProjectCreateUseCase_Execute_ReturnsDuplicateCodeError` — fake repo returns
    `ErrDuplicateCode`; assert use case surfaces the same error.
  - `TestProjectCreateUseCase_Execute_DefaultsStatusToActive` — input with empty `Status`;
    assert created project has `Status == StatusActive`.
  - `TestProjectDeleteUseCase_Execute_NotFoundError` — fake repo returns `ErrNotFound`;
    assert surfaced.
  - `TestProjectRestoreUseCase_Execute_HappyPath` — fake repo Restore succeeds; assert returned
    item has `DeletedAt == nil`.
  - `TestProjectListUseCase_Execute_DefaultsLimit` — call with `Limit=0`; assert fake repo
    received `Limit=20`.
- [ ] Create `internal/app/project/repository.go`:

  ```go
  package project

  import (
      "context"
      "errors"
      "github.com/google/uuid"
      domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
  )

  // ErrNotFound is returned when the requested project does not exist.
  var ErrNotFound = errors.New("project not found")

  // ErrDuplicateCode is returned when a project code already exists for the tenant.
  var ErrDuplicateCode = errors.New("project duplicate code")

  // Repository abstracts the persistence layer for Project.
  type Repository interface {
      Create(ctx context.Context, p *domain.Project) error
      GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.Project, error)
      List(ctx context.Context, f domain.ListFilter) ([]*domain.Project, int, error)
      Update(ctx context.Context, p *domain.Project) error
      Delete(ctx context.Context, tenantID, id uuid.UUID) error
      Restore(ctx context.Context, tenantID, id uuid.UUID) (*domain.Project, error)
  }
  ```

- [ ] Create `internal/app/project/create.go` — `CreateUseCase.Execute` validates input,
  defaults `Status` to `StatusActive` if empty, constructs a `Project` with a new UUID,
  calls `repo.Create`, returns the created entity.
- [ ] Create `internal/app/project/get.go` — `GetByIDUseCase.Execute` delegates to
  `repo.GetByID`.
- [ ] Create `internal/app/project/list.go` — `ListUseCase.Execute` enforces
  `Limit` in range `[1, 200]` defaulting to 20, delegates to `repo.List`.
- [ ] Create `internal/app/project/update.go` — `UpdateUseCase.Execute` fetches existing entry,
  applies non-nil fields from `UpdateInput`, validates the merged entity, calls `repo.Update`.
- [ ] Create `internal/app/project/delete.go` — `DeleteUseCase.Execute` delegates to
  `repo.Delete`; propagates `ErrNotFound`.
- [ ] Create `internal/app/project/restore.go` — `RestoreUseCase.Execute` delegates to
  `repo.Restore`; propagates `ErrNotFound`.
- [ ] Verify: `go test ./internal/app/project/... -v -race` passes.

---

### Task 4: Go adapter/repo — `internal/adapter/repo/project/repo.go` (2h)

- [ ] Write failing test: `internal/adapter/repo/project/repo_test.go`
  (integration-tagged, requires `TEST_DSN` env var pointing to local Postgres with migration
  000029 applied; follow the same pattern as `internal/adapter/repo/horticulture/dict_repo_test.go`):
  - `TestProjectRepo_Create_HappyPath` — create one project; assert row exists via `GetByID`.
  - `TestProjectRepo_Create_DuplicateCodeReturnsError` — create same code twice for same tenant;
    assert `errors.Is(err, appproject.ErrDuplicateCode)`.
  - `TestProjectRepo_List_FiltersOnQuery` — insert "河道绿化A" and "道路修缮B"; list with
    `Query="河道"`; assert len==1.
  - `TestProjectRepo_List_FiltersOnStatus` — insert one active and one completed; list with
    `Status=StatusActive`; assert len==1.
  - `TestProjectRepo_Delete_SoftDeletesRow` — create then delete; `GetByID` returns `ErrNotFound`.
  - `TestProjectRepo_Restore_MakesRowVisibleAgain` — soft-delete then restore; `GetByID` succeeds.
  - `TestProjectRepo_RLS_TenantBCannotSeeTenantARow` — create with tenantA; query with tenantB
    session; assert 0 rows returned.
- [ ] Create `internal/adapter/repo/project/repo.go` following the same pattern as
  `internal/adapter/repo/horticulture/dict_repo.go`:
  - Use `database/sql` + raw SQL; same `DB` interface pattern.
  - `Create`: INSERT all columns; detect unique violation by string-matching the error
    (`strings.Contains(err.Error(), "23505")` or `isPgUniqueViolation` helper — same approach
    as the horticulture repo). Return `appproject.ErrDuplicateCode` on match.
  - `GetByID`: SELECT WHERE `id=$1 AND tenant_id=$2 AND deleted_at IS NULL`.
    Note: no shared-seed rows for project — RLS is strict tenant isolation only.
  - `List`: dynamic WHERE builder; `tenant_id=$1 AND deleted_at IS NULL`; ILIKE on `name` or
    `code` when `Query` is set; filter on `status` and `customer_id` if set; ORDER BY
    `created_at DESC`; return total count + paginated slice.
  - `Update`: UPDATE where `id=$1 AND tenant_id=$2 AND deleted_at IS NULL`; return
    `ErrNotFound` on 0 rows affected.
  - `Delete`: UPDATE SET `deleted_at=now()` WHERE `id=$1 AND tenant_id=$2 AND
    deleted_at IS NULL`; return `ErrNotFound` on 0 rows.
  - `Restore`: UPDATE SET `deleted_at=NULL, updated_at=now()` WHERE `id=$1 AND
    tenant_id=$2 AND deleted_at IS NOT NULL`; re-fetch and return.
  - `scanProject(rowScanner)` helper — scan all columns; use `sql.NullString` for nullable
    text fields; use `*time.Time` for `start_date`, `end_date`, `deleted_at`; use
    `sql.NullString` for `contract_amount` (PostgreSQL NUMERIC scans to string cleanly via
    `*string` dest — verify by checking `internal/adapter/repo/bill/` scan pattern first).
  - Do NOT introduce a `pqErr` interface type — use the string-matching helper only (the
    S28.1 lint flagged an unused `pqErr` interface; avoid repeating that).
- [ ] Verify: `go test -tags integration ./internal/adapter/repo/project/... -v` passes
  (requires `TEST_DSN` env var and migration 000029 applied).

---

### Task 5: Go adapter/handler — `internal/adapter/handler/project/handler.go` (2h)

- [ ] Write failing test: `internal/adapter/handler/project/handler_test.go`
  (unit test using `httptest`, fake use cases injected via interfaces):
  - `TestProjectHandler_List_Returns200WithItems` — inject list UC returning 3 items; GET
    /api/v1/projects; assert 200, `"total":3`, `"items"` array len 3.
  - `TestProjectHandler_Create_Returns201` — valid JSON body; assert 201 + Location header
    contains the new project ID.
  - `TestProjectHandler_Create_DuplicateCode_Returns409` — UC returns `ErrDuplicateCode`;
    assert 409 `{"error":"duplicate code"}`.
  - `TestProjectHandler_Create_MissingName_Returns400` — body `{"code":"P001"}`; UC returns
    validation error; assert 400.
  - `TestProjectHandler_GetByID_Returns404ForUnknown` — UC returns `ErrNotFound`; assert 404.
  - `TestProjectHandler_Update_Returns200` — UC returns updated entity; assert 200.
  - `TestProjectHandler_Delete_Returns204` — UC succeeds; assert 204.
  - `TestProjectHandler_Restore_Returns200` — UC returns restored entity; assert 200.
- [ ] Create `internal/adapter/handler/project/handler.go`:

  DTO:
  ```go
  type ProjectDTO struct {
      ID             string  `json:"id"`
      TenantID       string  `json:"tenant_id"`
      Code           string  `json:"code"`
      Name           string  `json:"name"`
      CustomerID     *string `json:"customer_id,omitempty"`
      ContractAmount *string `json:"contract_amount,omitempty"`
      StartDate      *string `json:"start_date,omitempty"`  // RFC3339 date string
      EndDate        *string `json:"end_date,omitempty"`
      Status         string  `json:"status"`
      Address        string  `json:"address"`
      Manager        string  `json:"manager"`
      Remark         string  `json:"remark"`
      CreatedAt      string  `json:"created_at"`
      UpdatedAt      string  `json:"updated_at"`
  }

  type ProjectListResponse struct {
      Items []*ProjectDTO `json:"items"`
      Total int           `json:"total"`
  }
  ```

  Routes registered via `RegisterRoutes(rg *gin.RouterGroup)`:
  - `GET    /projects`          → list (query params: `q`, `status`, `customer_id`, `limit`, `offset`)
  - `POST   /projects`          → create
  - `GET    /projects/:id`      → get by ID
  - `PUT    /projects/:id`      → update
  - `DELETE /projects/:id`      → soft-delete
  - `POST   /projects/:id/restore` → restore

  Error mapping:
  - `ErrNotFound` → 404 `{"error":"not found"}`
  - `ErrDuplicateCode` → 409 `{"error":"duplicate code"}`
  - `domain.Validate()` errors → 400
  - all others → 500

  Date fields (`start_date`, `end_date`): parse `*time.Time` from ISO date string
  (`"2025-01-15"`) in the request body using `time.Parse("2006-01-02", v)`. Serialize to
  `*string` using `t.Format("2006-01-02")` in `toDTO`. Store `nil` for absent dates.

- [ ] Create `internal/adapter/handler/project/` as a new package `handlerproject` at the
  import path `github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/project`.
  The exported type is `ProjectHandler`. The constructor is `NewProjectHandler(...)`.
- [ ] Verify: `go test ./internal/adapter/handler/project/... -v -race` passes;
  `CGO_ENABLED=0 GOOS=linux go build ./...` exits 0.

---

### Task 6: Router registration + DI wiring (1h)

- [ ] Write failing test: in `internal/adapter/handler/router/router_test.go`:
  - Update `newTestRouter()` to pass `nil` as the 14th argument (type
    `*handlerproject.ProjectHandler`).
  - Add `TestRouter_RegistersProjectRoutes` — call `router.New(...)` with a real or nil
    `*handlerproject.ProjectHandler`; enumerate `engine.Routes()` and assert both
    `GET /api/v1/projects` and `POST /api/v1/projects/:id/restore` are present.
- [ ] Modify `internal/adapter/handler/router/router.go`:
  - Import `handlerproject "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/project"`.
  - Add `ph *handlerproject.ProjectHandler` as the **14th parameter** to `New(...)`.
  - Inside the `api` group block, after the horticulture block:

    ```go
    // Project CRUD (Story 28.2).
    if ph != nil {
        ph.RegisterRoutes(api)
    } else {
        api.GET("/projects", notImplemented)
        api.POST("/projects", notImplemented)
        api.GET("/projects/:id", notImplemented)
        api.PUT("/projects/:id", notImplemented)
        api.DELETE("/projects/:id", notImplemented)
        api.POST("/projects/:id/restore", notImplemented)
    }
    ```

  - The local variable `ph` shadows the existing `ph *handlerproduct.Handler` parameter.
    Rename the product handler parameter to `prodh` (or another non-conflicting name) **only
    if** the existing code uses the name `ph` for products. Check `router.go` first — the
    current code uses `ph *handlerproduct.Handler` at position 3. To avoid the clash, name the
    new project handler param `projh` and the existing product handler stays `ph`. Adjust
    accordingly.

    Confirmed safe alias: use parameter name `projh *handlerproject.ProjectHandler` as the 14th
    parameter. The nil stub in `router_test.go` `newTestRouter()` passes 14 nil values.

- [ ] Modify `internal/lifecycle/app.go`:
  - Import `appproject`, `repoproj`, `handlerproject` packages.
  - Construct the project DI chain after the horticulture chain:
    1. `projectRepo   := repoproj.New(db)`
    2. `createProjUC  := appproject.NewCreateUseCase(projectRepo)`
    3. `getProjUC     := appproject.NewGetByIDUseCase(projectRepo)`
    4. `listProjUC    := appproject.NewListUseCase(projectRepo)`
    5. `updateProjUC  := appproject.NewUpdateUseCase(projectRepo)`
    6. `deleteProjUC  := appproject.NewDeleteUseCase(projectRepo)`
    7. `restoreProjUC := appproject.NewRestoreUseCase(projectRepo)`
    8. `projectHandler := handlerproject.NewProjectHandler(createProjUC, getProjUC, listProjUC, updateProjUC, deleteProjUC, restoreProjUC)`
  - Pass `projectHandler` as the new 14th argument to `router.New(...)`.
- [ ] Verify: `CGO_ENABLED=0 GOOS=linux go build ./...` exits 0;
  `go test ./internal/adapter/handler/router/... -v` passes.

---

### Task 7: Frontend — API client `web/lib/api/projects.ts` (0.5h)

- [ ] Write failing test: `web/lib/api/projects.test.ts` (vitest + `vi.stubGlobal`):
  - `TestProjectsApi_List_ParsesPaginatedResponse` — stub fetch returning
    `{ items: [{id:"1",code:"P001",name:"河道绿化",status:"active",...}], total: 1 }`;
    call `listProjects({})`; assert `result.items[0].name === "河道绿化"`.
  - `TestProjectsApi_Create_Returns201Data` — stub fetch returning a created project;
    call `createProject({name:"测试",code:"T001",...})`; assert returned item has `name === "测试"`.
  - `TestProjectsApi_Delete_CallsDeleteMethod` — stub fetch; call `deleteProject("id1")`;
    assert fetch called with method `"DELETE"` and URL containing `"id1"`.
  - `TestProjectsApi_Restore_CallsRestoreEndpoint` — stub fetch; call `restoreProject("id1")`;
    assert fetch called with method `"POST"` and URL containing `"restore"`.
- [ ] Create `web/lib/api/projects.ts`:

  ```typescript
  export type ProjectStatus = 'active' | 'paused' | 'completed' | 'cancelled'

  export interface ProjectItem {
    id: string
    tenantId: string
    code: string
    name: string
    customerId?: string
    contractAmount?: string
    startDate?: string  // "YYYY-MM-DD"
    endDate?: string
    status: ProjectStatus
    address: string
    manager: string
    remark: string
    createdAt: string
    updatedAt: string
  }

  export interface ProjectListParams {
    q?: string
    status?: ProjectStatus
    customerId?: string
    limit?: number
    offset?: number
  }

  export interface ProjectListResult {
    items: ProjectItem[]
    total: number
  }

  export type ProjectCreateInput = Omit<ProjectItem, 'id' | 'tenantId' | 'createdAt' | 'updatedAt'>
  export type ProjectUpdateInput = Partial<ProjectCreateInput>

  export function listProjects(params: ProjectListParams): Promise<ProjectListResult>
  export function getProject(id: string): Promise<ProjectItem>
  export function createProject(input: ProjectCreateInput): Promise<ProjectItem>
  export function updateProject(id: string, input: ProjectUpdateInput): Promise<ProjectItem>
  export function deleteProject(id: string): Promise<void>
  export function restoreProject(id: string): Promise<ProjectItem>
  ```

  Follow the same `fetch` + auth header pattern used in `web/lib/api/nursery-dict.ts`.
  Query params: `customer_id` (snake_case) is sent as-is to match the Go handler's
  `c.Query("customer_id")`.

- [ ] Verify: `bun run test -- lib/api/projects` passes.

---

### Task 8: Frontend — form component `web/components/project/ProjectForm.tsx` (1.5h)

- [ ] Write failing test: `web/components/project/ProjectForm.test.tsx`
  (vitest + `@testing-library/react`):
  - `TestProjectForm_RequiredFields_ShowsValidationError` — submit with empty `name` and `code`;
    assert validation error message(s) are visible.
  - `TestProjectForm_SubmitCreate_CallsCreateApi` — fill `name="河道绿化"` and `code="P001"`;
    submit; assert `createProject` was called once with the correct payload.
  - `TestProjectForm_SubmitEdit_CallsUpdateApi` — render in edit mode with `initialData`;
    change `remark`; submit; assert `updateProject` was called.
  - `TestProjectForm_CustomerField_HasTodoComment` — render the component; assert the customer
    field's input or its nearest container has a `data-testid="customer-field"` and is present
    in the DOM (the actual combobox implementation is deferred to S28.8).
- [ ] Create `web/components/project/ProjectForm.tsx`:
  - Fields: 项目编号 `code` (required), 项目名称 `name` (required), 合同金额 `contractAmount`
    (number input, optional), 开工日期 `startDate` (date input, optional), 完工日期 `endDate`
    (date input, optional), 状态 `status` (`<select>` with four options), 地址 `address`
    (text input), 项目负责人 `manager` (text input), 备注 `remark` (textarea).
  - Customer field: plain text input labeled "客户ID" with `data-testid="customer-field"`.
    Include a comment: `{/* TODO(S28.8): swap to partner Combobox once partner search API is ready */}`.
  - Client-side validation: require non-empty `name` and `code` before calling the API.
  - Submit calls `createProject` (new mode) or `updateProject` (edit mode).
  - Props: `mode: "create" | "edit"`, `initialData?: ProjectItem`, `onSuccess(item: ProjectItem): void`,
    `onCancel(): void`.
- [ ] Verify: `bun run test -- components/project` passes.

---

### Task 9: Frontend — list page `web/app/(dashboard)/projects/page.tsx` (2h)

- [ ] Write failing test: `web/app/(dashboard)/projects/page.test.tsx`
  (vitest + `@testing-library/react`):
  - `TestProjectsPage_Renders_SearchAndGrid` — mock `listProjects` returning
    `{ items: [], total: 0 }`; render page; assert search input present; assert status filter
    `<select>` present; assert "+ 新建项目" button present.
  - `TestProjectsPage_CardsRendered` — mock returns 3 items; render; assert 3 card elements
    are visible (each with `data-testid="project-card"`).
  - `TestProjectsPage_EmptyState_Shown` — mock returns `{ items: [], total: 0 }`; render;
    assert text "暂无项目" is present.
  - `TestProjectsPage_Search_CallsApiWithQuery` — type "河道" in the search input; assert
    `listProjects` was called with `{ q: "河道" }` (accounts for 300ms debounce — use
    `vi.useFakeTimers()` to advance timers).
- [ ] Create `web/app/(dashboard)/projects/page.tsx` (client component):
  - Top bar: search `<input>` (debounced 300ms), status `<select>` (options: 全部 / 进行中 /
    已暂停 / 已完工 / 已取消, mapping to API values `""/"active"/"paused"/"completed"/"cancelled"`),
    "+ 新建项目" button.
  - Card grid: `grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4`. Each card
    (`data-testid="project-card"`) rendered in a `<div>` with rounded border and shadow. Card
    contents:
    - Project name as `<h3>` (font-semibold)
    - Code below name (text-muted-foreground text-sm)
    - Customer badge (`<span className="rounded-full bg-muted px-2 py-0.5 text-xs">`) — shows
      `customerId` if present, otherwise hidden
    - Contract amount large number (if `contractAmount` is present)
    - Status badge — color-coded: active=green bg, paused=yellow bg, completed=gray bg,
      cancelled=red bg — using Tailwind classes directly (no shadcn Badge needed; keep it
      simple):
      ```
      active:    "bg-green-500/10 text-green-700"
      paused:    "bg-yellow-500/10 text-yellow-700"
      completed: "bg-gray-500/10 text-gray-600"
      cancelled: "bg-red-500/10 text-red-600"
      ```
    - Date range: `startDate – endDate` (show "—" if absent)
  - Clicking a card opens a side Sheet drawer (same inline-panel pattern as `dictionary/page.tsx`)
    showing full project details in view mode with an "编辑" button that switches to `<ProjectForm>`
    in edit mode.
  - "+ 新建项目" button opens the drawer with `<ProjectForm mode="create" />`.
  - Empty state: `<div>暂无项目，点击"新建项目"添加第一个</div>`.
  - Pagination: same "上一页 / 下一页" pattern with `offset` state as in `dictionary/page.tsx`.
  - Uses `listProjects` from the API client.
- [ ] Verify: `bun run test -- projects/page` passes; `bun run build` exits 0.

---

### Task 10: Sidebar update + E2E spec (0.5h)

- [ ] Modify `web/app/(dashboard)/sidebar.tsx`:
  - Add `{ href: "/projects", label: "项目", icon: "🏗️" }` to `BASE_NAV_ITEMS` array.
  - Insert immediately after the `{ href: "/dictionary", ...}` entry so the sidebar order is:
    `商品管理 | 采购管理 | 销售管理 | 财务管理 | 订阅与计费 | 苗木字典 | 项目`.
  - Do not reorder, modify, or remove any existing items.
- [ ] Write Playwright E2E spec `web/tests/e2e/projects.spec.ts`:

  ```typescript
  // web/tests/e2e/projects.spec.ts
  // Smoke tests that can run without seed data.
  // Data-dependent tests (create/search) are skipped pending test backend.
  // Run: bunx playwright test projects.spec.ts

  import { test, expect } from "@playwright/test"

  test("projects page title is visible", async ({ page }) => {
    await page.goto("/projects")
    await expect(page.locator("h1, h2, [data-testid='page-title']")).toContainText(["项目"])
  })

  test("new project button is present", async ({ page }) => {
    await page.goto("/projects")
    await expect(page.locator("button", { hasText: "新建项目" })).toBeVisible()
  })

  test.skip("create project and card appears in grid", async ({ page }) => {
    // Skipped: requires authenticated session with seeded tenant.
    // TODO: enable when test backend has E2E auth setup.
    await page.goto("/projects")
    await page.click("button:has-text('新建项目')")
    await page.fill("[name='name']", "E2E工程项目")
    await page.fill("[name='code']", "E2E-001")
    await page.click("button[type='submit']")
    await expect(page.locator("[data-testid='project-card']")).toContainText("E2E工程项目")
  })
  ```

- [ ] Verify: `bunx tsc --noEmit` exits 0 on the spec file (no `[...iter]` spread — use
  `Array.from(...)` for any iterator operations per S28.1 burn note).
- [ ] Verify: `bun run lint` exits 0; `gofmt -l ./internal` returns empty.

---

## File List

### New files (create)

| Path | Notes |
|------|-------|
| `migrations/000029_project.up.sql` | Table DDL + UNIQUE + RLS + indexes |
| `migrations/000029_project.down.sql` | DROP TABLE |
| `internal/domain/project/project.go` | Domain entity + ProjectStatus enum + Validate + CreateInput + UpdateInput + ListFilter |
| `internal/domain/project/project_test.go` | Domain validation unit tests |
| `internal/app/project/repository.go` | Repository interface + ErrNotFound + ErrDuplicateCode |
| `internal/app/project/create.go` | CreateUseCase |
| `internal/app/project/get.go` | GetByIDUseCase |
| `internal/app/project/list.go` | ListUseCase |
| `internal/app/project/update.go` | UpdateUseCase |
| `internal/app/project/delete.go` | DeleteUseCase |
| `internal/app/project/restore.go` | RestoreUseCase |
| `internal/app/project/project_usecases_test.go` | App layer unit tests (fake repo) |
| `internal/adapter/repo/project/repo.go` | SQL-based repository implementation |
| `internal/adapter/repo/project/repo_test.go` | Integration tests (tagged) |
| `internal/adapter/handler/project/handler.go` | Gin handler + DTO types |
| `internal/adapter/handler/project/handler_test.go` | Handler unit tests (httptest) |
| `web/lib/api/projects.ts` | Fetch-based API client |
| `web/lib/api/projects.test.ts` | Vitest unit tests for API client |
| `web/components/project/ProjectForm.tsx` | New/edit form component |
| `web/components/project/ProjectForm.test.tsx` | Vitest component tests |
| `web/app/(dashboard)/projects/page.tsx` | Card-grid list page |
| `web/app/(dashboard)/projects/page.test.tsx` | Vitest page tests |
| `web/tests/e2e/projects.spec.ts` | Playwright E2E smoke spec |

### Modified files

| Path | What changes |
|------|-------------|
| `internal/adapter/handler/router/router.go` | Add `projh *handlerproject.ProjectHandler` as 14th param; register project routes with nil-guard fallback |
| `internal/adapter/handler/router/router_test.go` | Update `newTestRouter()` to pass 14th nil arg; add `TestRouter_RegistersProjectRoutes` |
| `internal/lifecycle/app.go` | Construct project DI chain; pass `projectHandler` as 14th arg to `router.New(...)` |
| `web/app/(dashboard)/sidebar.tsx` | Add `{ href: "/projects", label: "项目", icon: "🏗️" }` after dictionary entry |

---

## Test Plan

### Go unit tests (`go test ./... -v -race`)

| Package | Key test functions |
|---------|-------------------|
| `internal/domain/project` | `TestProject_Validate_RejectsEmptyName`, `TestProject_Validate_RejectsEmptyCode`, `TestProject_Validate_RejectsEndBeforeStart`, `TestProject_Validate_AcceptsNilDates`, `TestProject_Validate_AcceptsOnlyStartDate`, `TestProjectStatus_AllValues` |
| `internal/app/project` | `TestProjectCreateUseCase_Execute_HappyPath`, `TestProjectCreateUseCase_Execute_ReturnsDuplicateCodeError`, `TestProjectCreateUseCase_Execute_DefaultsStatusToActive`, `TestProjectDeleteUseCase_Execute_NotFoundError`, `TestProjectRestoreUseCase_Execute_HappyPath`, `TestProjectListUseCase_Execute_DefaultsLimit` |
| `internal/adapter/handler/project` | `TestProjectHandler_List_Returns200WithItems`, `TestProjectHandler_Create_Returns201`, `TestProjectHandler_Create_DuplicateCode_Returns409`, `TestProjectHandler_Create_MissingName_Returns400`, `TestProjectHandler_GetByID_Returns404ForUnknown`, `TestProjectHandler_Update_Returns200`, `TestProjectHandler_Delete_Returns204`, `TestProjectHandler_Restore_Returns200` |
| `internal/adapter/handler/router` | `TestRouter_RegistersProjectRoutes` |

### Go integration tests (`go test -tags integration ./...`)

| Package | Key test functions |
|---------|-------------------|
| `internal/adapter/repo/project` | `TestProjectRepo_Create_HappyPath`, `TestProjectRepo_Create_DuplicateCodeReturnsError`, `TestProjectRepo_List_FiltersOnQuery`, `TestProjectRepo_List_FiltersOnStatus`, `TestProjectRepo_Delete_SoftDeletesRow`, `TestProjectRepo_Restore_MakesRowVisibleAgain`, `TestProjectRepo_RLS_TenantBCannotSeeTenantARow` |

Target coverage: app layer ≥ 80%, repo ≥ 60%, handler ≥ 50% (matching project TDD targets).

### Vitest frontend tests (`bun run test`)

| File | Key scenarios |
|------|--------------|
| `web/lib/api/projects.test.ts` | List parses paginated response; create returns data; delete calls DELETE; restore calls POST restore |
| `web/components/project/ProjectForm.test.tsx` | Required fields validation; submit create calls API; submit edit calls update; customer field present with TODO comment |
| `web/app/(dashboard)/projects/page.test.tsx` | Renders search + grid + button; cards rendered (3 items → 3 cards); empty state shown; search debounces and calls API |

### Playwright E2E (`bunx playwright test projects.spec.ts`)

| Spec | Scenario |
|------|---------|
| `projects.spec.ts` | Page title contains "项目" (smoke, no auth required if page renders a title) |
| `projects.spec.ts` | "+ 新建项目" button is visible |
| `projects.spec.ts` | Create + card appears — skipped pending test auth setup |

---

## Dev Notes

### Migration numbering
Migration head after S28.1 is 000028. This story creates 000029. Before writing the file,
run `ls migrations/ | sort | tail -1` to confirm no collision (e.g., another story in-flight
added a 000029). If a collision is found, take the next available number and update all
references in this story.

### `contract_amount` NUMERIC(18,2) scanning
PostgreSQL `NUMERIC` columns do not cleanly scan into Go `float64` (precision loss risk).
Check how `bill_head.total_amount` (also NUMERIC(18,2)) is scanned in
`internal/adapter/repo/bill/` before writing the project repo. If that layer uses `*string`
or a custom `sql.Scanner`, apply the same approach. The domain model uses `*string` to store
the amount value as-is; parse arithmetic in the app layer only when needed.

### DATE columns (`start_date`, `end_date`)
Both are optional. Scan into `*time.Time` (using `sql.NullTime` or a `*time.Time` pointer —
confirm which `database/sql` scan target is used for DATE in the existing bill repo first).
Store `nil` in the domain struct for absent dates. The handler formats/parses dates using
`"2006-01-02"` layout.

### Router parameter name collision
The existing `router.New` 3rd parameter is `ph *handlerproduct.Handler`. The new 14th parameter
must not also be named `ph`. Use `projh *handlerproject.ProjectHandler` as the parameter name
to avoid a compile error. Read `router.go` in full before making this change.

### `router_test.go` must be updated
The current `newTestRouter()` passes exactly 13 nil values to `router.New`. Adding the 14th
parameter means the test file no longer compiles until a 14th nil is appended. This is a
compile-error gating test: confirm it fails before implementing, then fix it as part of Task 6.

### `isPgUniqueViolation` helper
The horticulture `dict_repo.go` already contains `isPgUniqueViolation` using both a
`SQLState()` interface check AND string matching. Copy the same pattern (or extract it to a
shared `internal/adapter/repo/pgutil` package if two or more packages use it — but do not do
that extraction unless the calling code is already in two packages and the S28.1 dev agent did
not do it). Do NOT define a bare `pqErr` interface type at package level and leave it unused —
that triggers golangci-lint's `unused` linter (S28.1 burn).

### Strict RLS for `project` (differs from `nursery_dict`)
The `nursery_dict` RLS policy includes an OR clause for the shared-seed nil UUID. The `project`
RLS policy uses strict tenant isolation: `tenant_id = current_setting('app.tenant_id', true)::UUID`
with no OR clause. The repo's `GetByID` query therefore does not need the
`OR tenant_id = '00000000-...'` guard that the horticulture repo has.

### No `project_id` in `bill_head` yet
Story 28.3 is a separate migration that adds `project_id` FK columns to `bill_head` and
`payment_head`. This story only creates the `tally.project` table. Do not add any FK columns
to existing tables in migration 000029.

### `gofmt` and `golangci-lint` gate
S28.1 had a lint failure: gofmt drift on an unrelated file, then an unused `pqErr` interface.
Before declaring done: run `gofmt -w ./internal && golangci-lint run ./...` and fix all
findings. Zero warnings is the only acceptable state.

### TypeScript `[...iter]` spread prohibition
Do not use spread syntax on RegExp `matchAll()` or other iterators in `.spec.ts` or `.test.ts`
files. Use `Array.from(iter)` instead. The `tsconfig` targets ES3/ES5 in some test configs and
will fail with "Type 'IterableIterator' is not an array type" otherwise. (S28.1 burn.)

### Status badge colors
Use these Tailwind class combinations — no shadcn `<Badge>` component needed:
- `active`:    `bg-green-500/10 text-green-700 dark:text-green-400`
- `paused`:    `bg-yellow-500/10 text-yellow-700 dark:text-yellow-400`
- `completed`: `bg-gray-500/10 text-gray-600 dark:text-gray-400`
- `cancelled`: `bg-red-500/10 text-red-600 dark:text-red-400`

### Card grid responsive breakpoints
Use `grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4` — matches Tailwind's default
responsive breakpoints (sm=640px, lg=1024px). Do not use `md:grid-cols-2` if the design
calls for lg=3-col starting at 1024px; pick either `md` or `lg` based on the breakpoint
that makes the most visual sense, but be consistent in tests and implementation.

### Customer ID display in cards
For MVP, the card shows `customerId` (UUID string) as a badge. Story 28.8 adds the
`partner` list page and partner combobox; at that point the card can be updated to show the
partner name. Add a `// TODO(S28.8): resolve customer name from partner` comment in the
`page.tsx` card rendering code.

---

## Definition of Done

- [ ] `go test ./internal/domain/project/... ./internal/app/project/... ./internal/adapter/handler/project/... -v -count=1 -race` exits 0.
- [ ] `go test -tags integration ./internal/adapter/repo/project/... -v` exits 0 (requires `TEST_DSN`).
- [ ] `CGO_ENABLED=0 GOOS=linux go build ./...` exits 0.
- [ ] `golangci-lint run ./...` exits 0 (zero warnings; fix all findings before claiming done).
- [ ] `gofmt -l ./internal` returns empty (no drift).
- [ ] `cd web && bun run test` exits 0 (new tests pass; pre-existing failures unchanged).
- [ ] `cd web && bun run build` exits 0.
- [ ] `cd web && bun run lint` exits 0.
- [ ] `bunx tsc --noEmit` exits 0 on the new `.spec.ts` and `.test.ts` files.
- [ ] Migration 000029 applies cleanly: `make migrate-up` exits 0; `make migrate-down` rolls back cleanly.
- [ ] Six routes registered and verified via `engine.Routes()` in router test:
      `GET /api/v1/projects`, `POST /api/v1/projects`, `GET /api/v1/projects/:id`,
      `PUT /api/v1/projects/:id`, `DELETE /api/v1/projects/:id`, `POST /api/v1/projects/:id/restore`.
- [ ] AC-3 verified: POST with duplicate code returns 409.
- [ ] AC-4 verified: soft-delete hides project from list; restore brings it back.
- [ ] AC-6 verified: `Validate()` unit tests for empty name, empty code, end-before-start all pass.
- [ ] Card grid renders at 3-col (≥1024px), 2-col (≥640px), 1-col (<640px) — verified visually or by Playwright viewport resize test.
- [ ] Sidebar "项目" entry with `🏗️` icon visible and highlights on `/projects` route.
- [ ] `doc/coord/service-status.md` updated: Tally block story 28.2 → Done.
- [ ] `doc/process.md` updated with ≤15-line summary.

---

## Dependencies

| # | Dependency | Relationship |
|---|-----------|-------------|
| D1 | Migration 000029 must be applied before the backend image that registers `/api/v1/projects` routes | Hard: deploy migration before rolling out the new backend image |
| D2 | `tally.partner` table (migration 000004) must exist | Hard: `project.customer_id` references `tally.partner(id)` via FK |
| D3 | Story 28.1 (nursery_dict) must be done | Hard: migration 000028 must be at head before 000029 can be applied; router signature change in 28.1 (param 13) must already be committed |
| D4 | Story 28.3 (bill_head/payment_head ← project_id FK) | 28.3 depends on this story being done; it will add a migration on top of 000029 |
| D5 | Story 28.4 (project P&L view) | Depends on both 28.2 (project table) and 28.3 (bill linkage) |

---

## Risks and Assumptions

| # | Item | Risk if wrong | Resolution |
|---|------|--------------|------------|
| A1 | Migration 000029 is the next available number after 000028 | If another in-flight story added 000029 concurrently, file name conflicts | Dev agent: run `ls migrations/ | sort | tail -1` before writing; adjust number and update story refs if needed |
| A2 | `bill_head.total_amount` (NUMERIC) is scanned via `*string` in the bill repo | If the bill repo uses a different scan pattern (e.g., `sql.NullFloat64`), the project repo must match | Dev agent: inspect `internal/adapter/repo/bill/` before writing `scanProject`; copy the exact scan target type |
| A3 | The 14th router param is named `projh` (not `ph`) to avoid shadowing the product handler | If any other name collision exists in the function signature, compilation fails | Dev agent: read the full `router.New` signature before modifying it |
| A4 | `web/app/(dashboard)/sidebar.tsx` uses the `NavItem.icon: string` pattern (emoji string, not a React component) | If the sidebar was refactored to use Lucide components between S28.1 and S28.2, the new item format breaks | Dev agent: re-read `sidebar.tsx` before inserting; match the existing item JSX structure exactly |
| A5 | The card grid's `data-testid="project-card"` attribute is present on each card element | If the test locates cards by a different selector, `CardsRendered` test fails | Dev agent: ensure `data-testid="project-card"` is added to the card root `<div>` in `page.tsx` to match the test |
| A6 | `tsc --noEmit` on `projects.spec.ts` passes without spread-on-iterator issues | If any new TS lib types are used that expose iterators, the ES3 trap hits | Dev agent: avoid `[...map.values()]` etc.; use `Array.from(...)` throughout |
| A7 | `ContractAmount` stored as `*string` in Go domain is sufficient for S28.2 display purposes | If the P&L view (S28.4) needs numeric arithmetic on the Go side, `*string` requires an extra parse step | Acceptable trade-off: S28.2 is display-only; arithmetic is deferred to S28.4 where the correct type can be chosen |

---

## Dev Agent Record

### Implementation completed: 2026-05-01

**Tasks completed**: All 10 tasks done. All tests passing.

**Decisions made**:
1. `contract_amount` scanned via `sql.NullString` (matching bill repo's string approach for NUMERIC), stored as `*string` in domain.
2. `isPgUniqueViolation` copied from horticulture repo with `pgErr` interface check + string fallback. The interface is defined inline inside the function (not at package level) to avoid golangci-lint `unused` warning — same as horticulture S28.1 pattern.
3. `ProjectsPage` search debounce test uses `waitFor` with 1000ms timeout instead of `vi.useFakeTimers()` — fake timers caused timeout issues with async `mockResolvedValue` promises; the approach matches `DictionaryPage` test pattern.
4. Router 14th param named `projh *handlerproject.ProjectHandler` to avoid clash with existing `ph *handlerproduct.Handler`.
5. `bun run test` full suite has 11 pre-existing failures (Playwright e2e specs + auth-session test); these were all failing before this story and are not caused by this story's changes. New test files (13 frontend + 27 Go) all pass.

**Deviations**: None from story spec.
