package horticulture_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	handlerhort "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/horticulture"
	apphort "github.com/hanmahong5-arch/lurus-tally/internal/app/horticulture"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
)

// ---- additional fake repository for coverage-focused tests ----
//
// Named distinctly from fakeHandlerRepo in dict_handler_test.go to avoid any
// symbol collisions within the same (external) test package.

type fakeRepoCov struct {
	createErr error
	created   *domain.NurseryDict

	getReturn *domain.NurseryDict
	getErr    error

	listItems  []*domain.NurseryDict
	listTotal  int
	listErr    error
	lastFilter domain.ListFilter

	updateErr    error
	updated      *domain.NurseryDict
	deleteErr    error
	restoreErr   error
	restoreValue *domain.NurseryDict
}

func (f *fakeRepoCov) Create(_ context.Context, d *domain.NurseryDict) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = d
	return nil
}

func (f *fakeRepoCov) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.NurseryDict, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getReturn != nil {
		return f.getReturn, nil
	}
	return &domain.NurseryDict{
		ID:        uuid.New(),
		TenantID:  uuid.Nil,
		Name:      "existing",
		Type:      domain.NurseryTypeTree,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

func (f *fakeRepoCov) List(_ context.Context, filter domain.ListFilter) ([]*domain.NurseryDict, int, error) {
	f.lastFilter = filter
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return f.listItems, f.listTotal, nil
}

func (f *fakeRepoCov) Update(_ context.Context, d *domain.NurseryDict) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updated = d
	return nil
}

func (f *fakeRepoCov) Delete(_ context.Context, _, _ uuid.UUID) error {
	return f.deleteErr
}

func (f *fakeRepoCov) Restore(_ context.Context, _, _ uuid.UUID) (*domain.NurseryDict, error) {
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	if f.restoreValue != nil {
		return f.restoreValue, nil
	}
	return &domain.NurseryDict{
		ID:        uuid.New(),
		Name:      "restored",
		Type:      domain.NurseryTypeTree,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

var _ apphort.Repository = (*fakeRepoCov)(nil)

func newCovRouter(repo apphort.Repository) *gin.Engine {
	h := handlerhort.NewDictHandler(
		apphort.NewCreateUseCase(repo),
		apphort.NewGetByIDUseCase(repo),
		apphort.NewListUseCase(repo),
		apphort.NewUpdateUseCase(repo),
		apphort.NewDeleteUseCase(repo),
		apphort.NewRestoreUseCase(repo),
	)
	r := gin.New()
	r.Use(gin.Recovery())
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return r
}

func doReq(r *gin.Engine, method, path string, body []byte) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---- List: query params ----

func TestCov_List_IsEvergreenTrueParsed(t *testing.T) {
	repo := &fakeRepoCov{listItems: nil, listTotal: 0}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodGet, "/api/v1/nursery-dict?is_evergreen=true", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if repo.lastFilter.IsEvergreen == nil || *repo.lastFilter.IsEvergreen != true {
		t.Errorf("IsEvergreen filter = %v, want pointer to true", repo.lastFilter.IsEvergreen)
	}
}

func TestCov_List_IsEvergreenFalseParsed(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodGet, "/api/v1/nursery-dict?is_evergreen=false", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if repo.lastFilter.IsEvergreen == nil || *repo.lastFilter.IsEvergreen != false {
		t.Errorf("IsEvergreen filter = %v, want pointer to false", repo.lastFilter.IsEvergreen)
	}
}

func TestCov_List_IsEvergreenGarbageLeavesFilterNil(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodGet, "/api/v1/nursery-dict?is_evergreen=notabool", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if repo.lastFilter.IsEvergreen != nil {
		t.Errorf("IsEvergreen filter = %v, want nil for unparsable value", repo.lastFilter.IsEvergreen)
	}
}

func TestCov_List_TypeQueryAppliedVerbatim(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodGet, "/api/v1/nursery-dict?type=shrub", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if repo.lastFilter.Type == nil || *repo.lastFilter.Type != domain.NurseryType("shrub") {
		t.Errorf("Type filter = %v, want shrub", repo.lastFilter.Type)
	}
}

func TestCov_List_LimitOffsetParsing(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		wantLimit  int
		wantOffset int
	}{
		{"valid both", "limit=5&offset=10", 5, 10},
		{"invalid limit falls back to default", "limit=abc", 20, 0},
		{"negative offset falls back to default", "offset=-5", 20, 0},
		{"no params default", "", 20, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepoCov{}
			r := newCovRouter(repo)
			path := "/api/v1/nursery-dict"
			if tc.query != "" {
				path += "?" + tc.query
			}
			w := doReq(r, http.MethodGet, path, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			if repo.lastFilter.Limit != tc.wantLimit {
				t.Errorf("Limit = %d, want %d", repo.lastFilter.Limit, tc.wantLimit)
			}
			if repo.lastFilter.Offset != tc.wantOffset {
				t.Errorf("Offset = %d, want %d", repo.lastFilter.Offset, tc.wantOffset)
			}
		})
	}
}

func TestCov_List_EmptyReturnsEmptySliceNotNull(t *testing.T) {
	repo := &fakeRepoCov{listItems: nil, listTotal: 0}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodGet, "/api/v1/nursery-dict", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// Assert raw body has "items":[] and not "items":null.
	if strings.Contains(w.Body.String(), `"items":null`) {
		t.Errorf("items serialized as null, want empty array; body=%s", w.Body.String())
	}
	var resp handlerhort.ListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Items == nil {
		t.Error("resp.Items is nil, want non-nil empty slice")
	}
	if len(resp.Items) != 0 {
		t.Errorf("len(Items) = %d, want 0", len(resp.Items))
	}
}

func TestCov_List_RepoError_Returns500(t *testing.T) {
	repo := &fakeRepoCov{listErr: errors.New("db exploded")}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodGet, "/api/v1/nursery-dict", nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// ---- Create ----

func TestCov_Create_TypeDefaultsToTreeWhenEmpty(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	body, _ := json.Marshal(map[string]any{"name": "无类型苗木"})
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if repo.created == nil {
		t.Fatal("repo.created is nil")
	}
	if repo.created.Type != domain.NurseryTypeTree {
		t.Errorf("Type = %q, want %q (default)", repo.created.Type, domain.NurseryTypeTree)
	}
}

func TestCov_Create_TypeExplicitPreserved(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	body, _ := json.Marshal(map[string]any{"name": "灌木", "type": "shrub"})
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if repo.created.Type != domain.NurseryTypeShrub {
		t.Errorf("Type = %q, want shrub", repo.created.Type)
	}
}

func TestCov_Create_InvalidJSON_Returns400(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict", []byte(`{"name":`))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_Create_MalformedSpecTemplate_Returns400(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	// spec_template value itself is syntactically invalid JSON (unquoted bareword),
	// so json.RawMessage (and the whole ShouldBindJSON call) fails to parse.
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict", []byte(`{"name":"x","spec_template":not-json}`))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_Create_ValidateError_InvalidBestSeason_Returns400(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	body, _ := json.Marshal(map[string]any{"name": "坏季节", "best_season": [2]int{0, 13}})
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict", body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if repo.created != nil {
		t.Error("repo.Create should not have been called for a validate failure")
	}
}

func TestCov_Create_DuplicateName_Returns409(t *testing.T) {
	repo := &fakeRepoCov{createErr: apphort.ErrDuplicateName}
	r := newCovRouter(repo)
	body, _ := json.Marshal(map[string]any{"name": "重复"})
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict", body)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_Create_DefaultUnitID_ValidUUIDParsed(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	unitID := uuid.New().String()
	body, _ := json.Marshal(map[string]any{"name": "带单位", "default_unit_id": unitID})
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if repo.created.DefaultUnitID == nil {
		t.Fatal("DefaultUnitID is nil, want parsed uuid")
	}
	if repo.created.DefaultUnitID.String() != unitID {
		t.Errorf("DefaultUnitID = %s, want %s", repo.created.DefaultUnitID.String(), unitID)
	}
}

func TestCov_Create_DefaultUnitID_InvalidSilentlyDroppedToNil(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	body, _ := json.Marshal(map[string]any{"name": "坏单位", "default_unit_id": "not-a-uuid"})
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (lenient parse should still proceed); body=%s", w.Code, w.Body.String())
	}
	if repo.created == nil {
		t.Fatal("repo.created is nil, create should have proceeded")
	}
	if repo.created.DefaultUnitID != nil {
		t.Errorf("DefaultUnitID = %v, want nil for invalid uuid input", repo.created.DefaultUnitID)
	}
}

func TestCov_Create_BestSeasonAndSpecTemplateRoundtrip(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	spec := json.RawMessage(`{"胸径_cm":10,"冠幅_cm":20}`)
	body, _ := json.Marshal(map[string]any{
		"name":          "圆柏",
		"best_season":   [2]int{3, 9},
		"spec_template": spec,
	})
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if repo.created.BestSeason != [2]int{3, 9} {
		t.Errorf("BestSeason = %v, want [3 9]", repo.created.BestSeason)
	}
	var dto handlerhort.NurseryDictDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if dto.BestSeason != [2]int{3, 9} {
		t.Errorf("dto.BestSeason = %v, want [3 9]", dto.BestSeason)
	}
	var gotSpec, wantSpec map[string]any
	_ = json.Unmarshal(dto.SpecTemplate, &gotSpec)
	_ = json.Unmarshal(spec, &wantSpec)
	if len(gotSpec) != len(wantSpec) {
		t.Errorf("spec_template roundtrip mismatch: got %s want %s", dto.SpecTemplate, spec)
	}
}

func TestCov_Create_EmptySpecTemplateNormalizedToEmptyObject(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	body, _ := json.Marshal(map[string]any{"name": "无模板"})
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var dto handlerhort.NurseryDictDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(dto.SpecTemplate) != "{}" {
		t.Errorf("SpecTemplate = %s, want {}", dto.SpecTemplate)
	}
}

func TestCov_Create_ClimateZonesNilBecomesEmptyArray(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	body, _ := json.Marshal(map[string]any{"name": "无气候带"})
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"climate_zones":null`) {
		t.Errorf("climate_zones serialized as null, want []; body=%s", w.Body.String())
	}
}

// ---- Create: binding max-length caps ----

func TestCov_Create_BindingMaxLengthCaps(t *testing.T) {
	long129 := strings.Repeat("a", 129)
	long501 := strings.Repeat("b", 501)

	cases := []struct {
		name  string
		field string
		value string
	}{
		{"name over 128", "name", long129},
		{"latin_name over 128", "latin_name", long129},
		{"family over 128", "family", long129},
		{"genus over 128", "genus", long129},
		{"type over 128", "type", long129},
		{"photo_url over 500", "photo_url", long501},
		{"remark over 500", "remark", long501},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepoCov{}
			r := newCovRouter(repo)
			payload := map[string]any{"name": "合法名称"}
			payload[tc.field] = tc.value
			body, _ := json.Marshal(payload)
			w := doReq(r, http.MethodPost, "/api/v1/nursery-dict", body)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 for %s over limit; body=%s", w.Code, tc.field, w.Body.String())
			}
		})
	}
}

func TestCov_Create_BindingAtLimitIsAccepted(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	body, _ := json.Marshal(map[string]any{
		"name":      strings.Repeat("a", 128),
		"photo_url": strings.Repeat("b", 500),
	})
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict", body)
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201 (at-limit values should pass); body=%s", w.Code, w.Body.String())
	}
}

// ---- GetByID ----

func TestCov_GetByID_InvalidUUID_Returns400(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodGet, "/api/v1/nursery-dict/not-a-uuid", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_GetByID_NotFound_Returns404(t *testing.T) {
	repo := &fakeRepoCov{getErr: apphort.ErrNotFound}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodGet, "/api/v1/nursery-dict/"+uuid.New().String(), nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_GetByID_RepoError_Returns500(t *testing.T) {
	repo := &fakeRepoCov{getErr: errors.New("connection reset")}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodGet, "/api/v1/nursery-dict/"+uuid.New().String(), nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_GetByID_Success_NormalizesNilSpecAndZones(t *testing.T) {
	repo := &fakeRepoCov{getReturn: &domain.NurseryDict{
		ID:           uuid.New(),
		TenantID:     uuid.Nil,
		Name:         "无字段",
		Type:         domain.NurseryTypeTree,
		SpecTemplate: nil,
		ClimateZones: nil,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodGet, "/api/v1/nursery-dict/"+uuid.New().String(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var dto handlerhort.NurseryDictDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(dto.SpecTemplate) != "{}" {
		t.Errorf("SpecTemplate = %s, want {}", dto.SpecTemplate)
	}
	if dto.ClimateZones == nil || len(dto.ClimateZones) != 0 {
		t.Errorf("ClimateZones = %v, want empty non-nil slice", dto.ClimateZones)
	}
}

func TestCov_GetByID_Success_WithDefaultUnitID(t *testing.T) {
	unitID := uuid.New()
	repo := &fakeRepoCov{getReturn: &domain.NurseryDict{
		ID:            uuid.New(),
		Name:          "有单位",
		Type:          domain.NurseryTypeTree,
		DefaultUnitID: &unitID,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodGet, "/api/v1/nursery-dict/"+uuid.New().String(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var dto handlerhort.NurseryDictDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.DefaultUnitID == nil || *dto.DefaultUnitID != unitID.String() {
		t.Errorf("DefaultUnitID = %v, want %s", dto.DefaultUnitID, unitID.String())
	}
}

// ---- Update ----

func TestCov_Update_InvalidUUID_Returns400(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	body, _ := json.Marshal(map[string]any{"name": "x"})
	w := doReq(r, http.MethodPut, "/api/v1/nursery-dict/not-a-uuid", body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_Update_InvalidJSON_Returns400(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodPut, "/api/v1/nursery-dict/"+uuid.New().String(), []byte(`{"name":`))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_Update_NotFound_Returns404(t *testing.T) {
	repo := &fakeRepoCov{getErr: apphort.ErrNotFound}
	r := newCovRouter(repo)
	body, _ := json.Marshal(map[string]any{"name": "x"})
	w := doReq(r, http.MethodPut, "/api/v1/nursery-dict/"+uuid.New().String(), body)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_Update_ValidateError_Returns400(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	badSeason := [2]int{0, 13}
	body, _ := json.Marshal(map[string]any{"best_season": badSeason})
	w := doReq(r, http.MethodPut, "/api/v1/nursery-dict/"+uuid.New().String(), body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if repo.updated != nil {
		t.Error("repo.Update should not have been called for a validate failure")
	}
}

func TestCov_Update_RepoUpdateError_Returns400(t *testing.T) {
	repo := &fakeRepoCov{updateErr: errors.New("write conflict")}
	r := newCovRouter(repo)
	body, _ := json.Marshal(map[string]any{"name": "更新名"})
	w := doReq(r, http.MethodPut, "/api/v1/nursery-dict/"+uuid.New().String(), body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (handler maps any non-NotFound update error to 400); body=%s", w.Code, w.Body.String())
	}
}

func TestCov_Update_BestSeasonAndSpecTemplateRoundtrip(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	spec := json.RawMessage(`{"a":1}`)
	newSeason := [2]int{4, 10}
	body, _ := json.Marshal(map[string]any{
		"best_season":   newSeason,
		"spec_template": spec,
	})
	w := doReq(r, http.MethodPut, "/api/v1/nursery-dict/"+uuid.New().String(), body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if repo.updated.BestSeason != newSeason {
		t.Errorf("updated.BestSeason = %v, want %v", repo.updated.BestSeason, newSeason)
	}
	if string(repo.updated.SpecTemplate) != string(spec) {
		t.Errorf("updated.SpecTemplate = %s, want %s", repo.updated.SpecTemplate, spec)
	}
}

func TestCov_Update_DefaultUnitID_ValidParsed(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	unitID := uuid.New().String()
	body, _ := json.Marshal(map[string]any{"default_unit_id": unitID})
	w := doReq(r, http.MethodPut, "/api/v1/nursery-dict/"+uuid.New().String(), body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if repo.updated.DefaultUnitID == nil || repo.updated.DefaultUnitID.String() != unitID {
		t.Errorf("updated.DefaultUnitID = %v, want %s", repo.updated.DefaultUnitID, unitID)
	}
}

func TestCov_Update_DefaultUnitID_InvalidSilentlyDropped(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	body, _ := json.Marshal(map[string]any{"default_unit_id": "garbage-uuid", "name": "改名"})
	w := doReq(r, http.MethodPut, "/api/v1/nursery-dict/"+uuid.New().String(), body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (lenient parse still proceeds); body=%s", w.Code, w.Body.String())
	}
	if repo.updated == nil {
		t.Fatal("repo.updated is nil, update should have proceeded")
	}
	// Invalid default_unit_id parses to nil pointer, which UpdateInput treats as
	// "do not touch" (existing GetByID stub returns a record with nil DefaultUnitID).
	if repo.updated.DefaultUnitID != nil {
		t.Errorf("updated.DefaultUnitID = %v, want nil (unchanged)", repo.updated.DefaultUnitID)
	}
}

func TestCov_Update_Success_ChangesName(t *testing.T) {
	repo := &fakeRepoCov{getReturn: &domain.NurseryDict{
		ID:        uuid.New(),
		Name:      "旧名",
		Type:      domain.NurseryTypeTree,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}}
	r := newCovRouter(repo)
	body, _ := json.Marshal(map[string]any{"name": "新名"})
	w := doReq(r, http.MethodPut, "/api/v1/nursery-dict/"+uuid.New().String(), body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if repo.updated.Name != "新名" {
		t.Errorf("updated.Name = %q, want 新名", repo.updated.Name)
	}
}

// ---- Delete ----

func TestCov_Delete_InvalidUUID_Returns400(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodDelete, "/api/v1/nursery-dict/not-a-uuid", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_Delete_NotFound_Returns404(t *testing.T) {
	repo := &fakeRepoCov{deleteErr: apphort.ErrNotFound}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodDelete, "/api/v1/nursery-dict/"+uuid.New().String(), nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_Delete_RepoError_Returns500(t *testing.T) {
	repo := &fakeRepoCov{deleteErr: errors.New("db timeout")}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodDelete, "/api/v1/nursery-dict/"+uuid.New().String(), nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_Delete_Success_Returns204(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodDelete, "/api/v1/nursery-dict/"+uuid.New().String(), nil)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want empty for 204", w.Body.String())
	}
}

// ---- Restore ----

func TestCov_Restore_InvalidUUID_Returns400(t *testing.T) {
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict/not-a-uuid/restore", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_Restore_NotFound_Returns404(t *testing.T) {
	repo := &fakeRepoCov{restoreErr: apphort.ErrNotFound}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict/"+uuid.New().String()+"/restore", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_Restore_RepoError_Returns500(t *testing.T) {
	repo := &fakeRepoCov{restoreErr: errors.New("db down")}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict/"+uuid.New().String()+"/restore", nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

func TestCov_Restore_Success_Returns200(t *testing.T) {
	repo := &fakeRepoCov{restoreValue: &domain.NurseryDict{
		ID:        uuid.New(),
		Name:      "复活的",
		Type:      domain.NurseryTypeTree,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}}
	r := newCovRouter(repo)
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict/"+uuid.New().String()+"/restore", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var dto handlerhort.NurseryDictDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.Name != "复活的" {
		t.Errorf("Name = %q, want 复活的", dto.Name)
	}
}

// ---- nil tenant reaches the use case (documented real behavior) ----

func TestCov_NilTenant_ReachesUseCase_NoAuthRejection(t *testing.T) {
	// No middleware sets the tenant context key in these tests, so
	// middleware.GetTenantID(c) resolves to uuid.Nil. The handler does not
	// 401/403 on that; it forwards uuid.Nil straight into the use case /
	// filter, relying on the repository (RLS in production) to scope
	// visibility. This test documents and locks in that real code path.
	repo := &fakeRepoCov{}
	r := newCovRouter(repo)
	body, _ := json.Marshal(map[string]any{"name": "无租户"})
	w := doReq(r, http.MethodPost, "/api/v1/nursery-dict", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 even with nil tenant; body=%s", w.Code, w.Body.String())
	}
	if repo.created.TenantID != uuid.Nil {
		t.Errorf("created.TenantID = %v, want uuid.Nil forwarded verbatim", repo.created.TenantID)
	}
}
