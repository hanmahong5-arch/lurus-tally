package horticulture_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	apphort "github.com/hanmahong5-arch/lurus-tally/internal/app/horticulture"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
)

// zzFakeRepo is a second hand-written fake implementing apphort.Repository,
// distinct from fakeRepo in dict_usecases_test.go (that file is not to be edited).
// It records the exact *domain.NurseryDict handed to Create/Update so tests can
// assert on default-fill / partial-update logic rather than only the return value.
type zzFakeRepo struct {
	createErr  error
	getErr     error
	getEntry   *domain.NurseryDict
	listErr    error
	listItems  []*domain.NurseryDict
	listTotal  int
	updateErr  error
	deleteErr  error
	restoreErr error
	restoreVal *domain.NurseryDict

	createCalled bool
	createArg    *domain.NurseryDict
	updateCalled bool
	updateArg    *domain.NurseryDict
	listFilter   domain.ListFilter
	deleteCalled bool
}

func (f *zzFakeRepo) Create(_ context.Context, d *domain.NurseryDict) error {
	f.createCalled = true
	f.createArg = d
	if f.createErr != nil {
		return f.createErr
	}
	return nil
}

func (f *zzFakeRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.NurseryDict, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getEntry != nil {
		return f.getEntry, nil
	}
	return &domain.NurseryDict{
		ID:   uuid.New(),
		Name: "松树",
		Type: domain.NurseryTypeTree,
	}, nil
}

func (f *zzFakeRepo) List(_ context.Context, filter domain.ListFilter) ([]*domain.NurseryDict, int, error) {
	f.listFilter = filter
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return f.listItems, f.listTotal, nil
}

func (f *zzFakeRepo) Update(_ context.Context, d *domain.NurseryDict) error {
	f.updateCalled = true
	f.updateArg = d
	if f.updateErr != nil {
		return f.updateErr
	}
	return nil
}

func (f *zzFakeRepo) Delete(_ context.Context, _, _ uuid.UUID) error {
	f.deleteCalled = true
	return f.deleteErr
}

func (f *zzFakeRepo) Restore(_ context.Context, _, _ uuid.UUID) (*domain.NurseryDict, error) {
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	if f.restoreVal != nil {
		return f.restoreVal, nil
	}
	return &domain.NurseryDict{ID: uuid.New(), Name: "松树"}, nil
}

// ---- CreateUseCase ----

func TestCreateUseCase_Execute_DefaultFillTypeAndSpecTemplate(t *testing.T) {
	repo := &zzFakeRepo{}
	uc := apphort.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "无类型苗木",
		// Type left empty, SpecTemplate left nil.
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.createCalled {
		t.Fatal("expected repo.Create to be called")
	}
	if repo.createArg.Type != domain.NurseryTypeTree {
		t.Errorf("expected default Type=%q, got %q", domain.NurseryTypeTree, repo.createArg.Type)
	}
	if string(repo.createArg.SpecTemplate) != "{}" {
		t.Errorf("expected default SpecTemplate='{}', got %q", string(repo.createArg.SpecTemplate))
	}
}

func TestCreateUseCase_Execute_PreservesExplicitTypeAndSpecTemplate(t *testing.T) {
	repo := &zzFakeRepo{}
	uc := apphort.NewCreateUseCase(repo)

	spec := json.RawMessage(`{"胸径_cm":null}`)
	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID:     uuid.New(),
		Name:         "灌木甲",
		Type:         domain.NurseryTypeShrub,
		SpecTemplate: spec,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.createArg.Type != domain.NurseryTypeShrub {
		t.Errorf("expected explicit Type preserved as %q, got %q", domain.NurseryTypeShrub, repo.createArg.Type)
	}
	if string(repo.createArg.SpecTemplate) != `{"胸径_cm":null}` {
		t.Errorf("expected explicit SpecTemplate preserved, got %q", string(repo.createArg.SpecTemplate))
	}
}

func TestCreateUseCase_Execute_ValidateFailure_RepoNotCalled(t *testing.T) {
	repo := &zzFakeRepo{}
	uc := apphort.NewCreateUseCase(repo)

	// Empty Name triggers domain.Validate failure ("name is required").
	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "",
	})
	if err == nil {
		t.Fatal("expected validate error, got nil")
	}
	wantPrefix := "nursery dict create validate:"
	if err.Error()[:len(wantPrefix)] != wantPrefix {
		t.Errorf("expected error wrapped with %q, got %q", wantPrefix, err.Error())
	}
	if repo.createCalled {
		t.Error("expected repo.Create NOT to be called on validate failure")
	}
}

func TestCreateUseCase_Execute_ValidateFailure_BadBestSeason(t *testing.T) {
	repo := &zzFakeRepo{}
	uc := apphort.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID:   uuid.New(),
		Name:       "季节错误",
		BestSeason: [2]int{0, 13}, // one non-zero out of range -> invalid
	})
	if err == nil {
		t.Fatal("expected validate error for bad best_season, got nil")
	}
	if repo.createCalled {
		t.Error("expected repo.Create NOT to be called on validate failure")
	}
}

func TestCreateUseCase_Execute_RepoNonDuplicateError_Wrapped(t *testing.T) {
	sentinel := errors.New("boom: connection refused")
	repo := &zzFakeRepo{createErr: sentinel}
	uc := apphort.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "普通错误",
	})
	if err == nil {
		t.Fatal("expected wrapped repo error, got nil")
	}
	if errors.Is(err, apphort.ErrDuplicateName) {
		t.Error("non-duplicate repo error must not be classified as ErrDuplicateName")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel error preserved via errors.Is, got %v", err)
	}
	wantPrefix := "nursery dict create:"
	if err.Error()[:len(wantPrefix)] != wantPrefix {
		t.Errorf("expected error wrapped with %q, got %q", wantPrefix, err.Error())
	}
}

func TestCreateUseCase_Execute_DuplicateNameViaErrorsIs(t *testing.T) {
	// Wrap ErrDuplicateName to ensure the use case unwraps via errors.Is, not ==.
	wrapped := errors.Join(apphort.ErrDuplicateName)
	repo := &zzFakeRepo{createErr: wrapped}
	uc := apphort.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "重复名",
	})
	if !errors.Is(err, apphort.ErrDuplicateName) {
		t.Errorf("expected ErrDuplicateName (possibly wrapped) to surface, got %v", err)
	}
}

// ---- GetByIDUseCase ----

func TestGetByIDUseCase_Execute_HappyPath(t *testing.T) {
	want := &domain.NurseryDict{ID: uuid.New(), Name: "香樟"}
	repo := &zzFakeRepo{getEntry: want}
	uc := apphort.NewGetByIDUseCase(repo)

	got, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("expected same entry pointer returned, got different value")
	}
}

func TestGetByIDUseCase_Execute_NotFoundMapped(t *testing.T) {
	repo := &zzFakeRepo{getErr: apphort.ErrNotFound}
	uc := apphort.NewGetByIDUseCase(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, apphort.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetByIDUseCase_Execute_OtherErrorWrapped(t *testing.T) {
	sentinel := errors.New("db timeout")
	repo := &zzFakeRepo{getErr: sentinel}
	uc := apphort.NewGetByIDUseCase(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if errors.Is(err, apphort.ErrNotFound) {
		t.Error("non-not-found repo error must not be classified as ErrNotFound")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel preserved, got %v", err)
	}
}

// ---- ListUseCase ----

func TestListUseCase_Execute_ClampingTable(t *testing.T) {
	cases := []struct {
		name      string
		inLimit   int
		wantLimit int
	}{
		{"zero_defaults_to_20", 0, 20},
		{"negative_defaults_to_20", -5, 20},
		{"within_range_unchanged", 50, 50},
		{"exactly_200_unchanged", 200, 200},
		{"over_200_clamped_to_200", 500, 200},
		{"one_is_not_floored_by_minLimit", 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &zzFakeRepo{}
			uc := apphort.NewListUseCase(repo)

			_, _, err := uc.Execute(context.Background(), domain.ListFilter{
				TenantID: uuid.New(),
				Limit:    tc.inLimit,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if repo.listFilter.Limit != tc.wantLimit {
				t.Errorf("Limit=%d: expected clamped %d, got %d", tc.inLimit, tc.wantLimit, repo.listFilter.Limit)
			}
		})
	}
}

func TestListUseCase_Execute_ReturnsItemsAndTotal(t *testing.T) {
	want := []*domain.NurseryDict{{ID: uuid.New(), Name: "竹子"}}
	repo := &zzFakeRepo{listItems: want, listTotal: 7}
	uc := apphort.NewListUseCase(repo)

	items, total, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.New(), Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 7 {
		t.Errorf("expected total=7, got %d", total)
	}
	if len(items) != 1 || items[0] != want[0] {
		t.Errorf("expected items passed through unchanged, got %+v", items)
	}
}

func TestListUseCase_Execute_RepoErrorWrapped(t *testing.T) {
	sentinel := errors.New("query failed")
	repo := &zzFakeRepo{listErr: sentinel}
	uc := apphort.NewListUseCase(repo)

	_, _, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.New()})
	if err == nil {
		t.Fatal("expected wrapped error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel preserved, got %v", err)
	}
	wantPrefix := "nursery dict list:"
	if err.Error()[:len(wantPrefix)] != wantPrefix {
		t.Errorf("expected error wrapped with %q, got %q", wantPrefix, err.Error())
	}
}

// ---- UpdateUseCase ----

func TestUpdateUseCase_Execute_GetByIDNotFound(t *testing.T) {
	repo := &zzFakeRepo{getErr: apphort.ErrNotFound}
	uc := apphort.NewUpdateUseCase(repo)

	name := "new name"
	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{Name: &name})
	if !errors.Is(err, apphort.ErrNotFound) {
		t.Errorf("expected ErrNotFound from GetByID, got %v", err)
	}
	if repo.updateCalled {
		t.Error("expected repo.Update NOT to be called when GetByID fails")
	}
}

func TestUpdateUseCase_Execute_GetByIDOtherErrorWrapped(t *testing.T) {
	sentinel := errors.New("db down")
	repo := &zzFakeRepo{getErr: sentinel}
	uc := apphort.NewUpdateUseCase(repo)

	name := "x"
	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{Name: &name})
	if errors.Is(err, apphort.ErrNotFound) {
		t.Error("non-not-found fetch error must not be classified as ErrNotFound")
	}
	wantPrefix := "nursery dict update fetch:"
	if err == nil || err.Error()[:len(wantPrefix)] != wantPrefix {
		t.Errorf("expected error wrapped with %q, got %v", wantPrefix, err)
	}
}

func TestUpdateUseCase_Execute_UpdateNotFound(t *testing.T) {
	repo := &zzFakeRepo{updateErr: apphort.ErrNotFound}
	uc := apphort.NewUpdateUseCase(repo)

	name := "y"
	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{Name: &name})
	if !errors.Is(err, apphort.ErrNotFound) {
		t.Errorf("expected ErrNotFound from Update, got %v", err)
	}
}

func TestUpdateUseCase_Execute_UpdateOtherErrorWrapped(t *testing.T) {
	sentinel := errors.New("write failed")
	repo := &zzFakeRepo{updateErr: sentinel}
	uc := apphort.NewUpdateUseCase(repo)

	name := "z"
	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{Name: &name})
	if errors.Is(err, apphort.ErrNotFound) {
		t.Error("non-not-found update error must not be classified as ErrNotFound")
	}
	wantPrefix := "nursery dict update:"
	if err == nil || err.Error()[:len(wantPrefix)] != wantPrefix {
		t.Errorf("expected error wrapped with %q, got %v", wantPrefix, err)
	}
}

func TestUpdateUseCase_Execute_ValidateAfterMergeFailure(t *testing.T) {
	repo := &zzFakeRepo{getEntry: &domain.NurseryDict{ID: uuid.New(), Name: "原名"}}
	uc := apphort.NewUpdateUseCase(repo)

	empty := ""
	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{Name: &empty})
	if err == nil {
		t.Fatal("expected validate error after merge (empty name), got nil")
	}
	wantPrefix := "nursery dict update validate:"
	if err.Error()[:len(wantPrefix)] != wantPrefix {
		t.Errorf("expected error wrapped with %q, got %q", wantPrefix, err.Error())
	}
	if repo.updateCalled {
		t.Error("expected repo.Update NOT to be called when post-merge validate fails")
	}
}

func TestUpdateUseCase_Execute_PartialUpdate_OnlyNonNilFieldsApplied(t *testing.T) {
	original := &domain.NurseryDict{
		ID:           uuid.New(),
		Name:         "原名",
		LatinName:    "Original Latinus",
		Family:       "原科",
		Genus:        "原属",
		Type:         domain.NurseryTypeShrub,
		IsEvergreen:  true,
		ClimateZones: []string{"8b"},
		BestSeason:   [2]int{3, 5},
		SpecTemplate: json.RawMessage(`{"existing":1}`),
		PhotoURL:     "https://old.example.com/a.jpg",
		Remark:       "旧备注",
	}
	repo := &zzFakeRepo{getEntry: original}
	uc := apphort.NewUpdateUseCase(repo)

	newName := "新名"
	d, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{
		Name: &newName,
		// all other pointer fields nil, ClimateZones nil, SpecTemplate nil/empty
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Name != "新名" {
		t.Errorf("expected Name updated to '新名', got %q", d.Name)
	}
	// Unset fields must remain unchanged.
	if d.LatinName != "Original Latinus" {
		t.Errorf("expected LatinName unchanged, got %q", d.LatinName)
	}
	if d.Family != "原科" {
		t.Errorf("expected Family unchanged, got %q", d.Family)
	}
	if d.Genus != "原属" {
		t.Errorf("expected Genus unchanged, got %q", d.Genus)
	}
	if d.Type != domain.NurseryTypeShrub {
		t.Errorf("expected Type unchanged, got %q", d.Type)
	}
	if !d.IsEvergreen {
		t.Error("expected IsEvergreen unchanged (true)")
	}
	if len(d.ClimateZones) != 1 || d.ClimateZones[0] != "8b" {
		t.Errorf("expected ClimateZones unchanged, got %v", d.ClimateZones)
	}
	if d.BestSeason != [2]int{3, 5} {
		t.Errorf("expected BestSeason unchanged, got %v", d.BestSeason)
	}
	if string(d.SpecTemplate) != `{"existing":1}` {
		t.Errorf("expected SpecTemplate unchanged (no-wipe), got %q", string(d.SpecTemplate))
	}
	if d.PhotoURL != "https://old.example.com/a.jpg" {
		t.Errorf("expected PhotoURL unchanged, got %q", d.PhotoURL)
	}
	if d.Remark != "旧备注" {
		t.Errorf("expected Remark unchanged, got %q", d.Remark)
	}
}

func TestUpdateUseCase_Execute_AllFieldsApplied(t *testing.T) {
	original := &domain.NurseryDict{
		ID:   uuid.New(),
		Name: "原名",
		Type: domain.NurseryTypeShrub,
	}
	repo := &zzFakeRepo{getEntry: original}
	uc := apphort.NewUpdateUseCase(repo)

	newName := "新名2"
	newLatin := "Novus Latinus"
	newFamily := "新科"
	newGenus := "新属"
	newType := domain.NurseryTypeBamboo
	newEvergreen := false
	newSeason := [2]int{6, 9}
	newPhoto := "https://new.example.com/b.jpg"
	newRemark := "新备注"
	newUnitID := uuid.New()
	newSpec := json.RawMessage(`{"新":true}`)

	d, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{
		Name:          &newName,
		LatinName:     &newLatin,
		Family:        &newFamily,
		Genus:         &newGenus,
		Type:          &newType,
		IsEvergreen:   &newEvergreen,
		ClimateZones:  []string{"9a", "9b"},
		BestSeason:    &newSeason,
		SpecTemplate:  newSpec,
		DefaultUnitID: &newUnitID,
		PhotoURL:      &newPhoto,
		Remark:        &newRemark,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Name != newName || d.LatinName != newLatin || d.Family != newFamily || d.Genus != newGenus {
		t.Errorf("expected string fields all applied, got %+v", d)
	}
	if d.Type != newType {
		t.Errorf("expected Type applied to %q, got %q", newType, d.Type)
	}
	if d.IsEvergreen != newEvergreen {
		t.Errorf("expected IsEvergreen applied to %v, got %v", newEvergreen, d.IsEvergreen)
	}
	if len(d.ClimateZones) != 2 || d.ClimateZones[0] != "9a" || d.ClimateZones[1] != "9b" {
		t.Errorf("expected ClimateZones applied, got %v", d.ClimateZones)
	}
	if d.BestSeason != newSeason {
		t.Errorf("expected BestSeason applied to %v, got %v", newSeason, d.BestSeason)
	}
	if string(d.SpecTemplate) != `{"新":true}` {
		t.Errorf("expected SpecTemplate applied, got %q", string(d.SpecTemplate))
	}
	if d.DefaultUnitID == nil || *d.DefaultUnitID != newUnitID {
		t.Errorf("expected DefaultUnitID applied to %v, got %v", newUnitID, d.DefaultUnitID)
	}
	if d.PhotoURL != newPhoto {
		t.Errorf("expected PhotoURL applied, got %q", d.PhotoURL)
	}
	if d.Remark != newRemark {
		t.Errorf("expected Remark applied, got %q", d.Remark)
	}
	if d.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set to now")
	}
	if !repo.updateCalled {
		t.Error("expected repo.Update to be called")
	}
	if repo.updateArg != d {
		t.Error("expected repo.Update to receive the same mutated entry pointer")
	}
}

func TestUpdateUseCase_Execute_EmptyClimateZonesSliceIsApplied(t *testing.T) {
	// Business rule: applied "when in.ClimateZones != nil" — a non-nil empty slice
	// still counts as an explicit clear, distinct from nil (no-op).
	original := &domain.NurseryDict{
		ID:           uuid.New(),
		Name:         "原名",
		Type:         domain.NurseryTypeTree,
		ClimateZones: []string{"7a"},
	}
	repo := &zzFakeRepo{getEntry: original}
	uc := apphort.NewUpdateUseCase(repo)

	empty := []string{}
	d, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{
		ClimateZones: empty,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ClimateZones == nil || len(d.ClimateZones) != 0 {
		t.Errorf("expected ClimateZones replaced with empty non-nil slice, got %#v", d.ClimateZones)
	}
}

func TestUpdateUseCase_Execute_ZeroLengthSpecTemplateDoesNotWipe(t *testing.T) {
	original := &domain.NurseryDict{
		ID:           uuid.New(),
		Name:         "原名",
		Type:         domain.NurseryTypeTree,
		SpecTemplate: json.RawMessage(`{"胸径_cm":12}`),
	}
	repo := &zzFakeRepo{getEntry: original}
	uc := apphort.NewUpdateUseCase(repo)

	newName := "新名3"
	d, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{
		Name:         &newName,
		SpecTemplate: json.RawMessage(``), // len==0, must not wipe existing SpecTemplate
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(d.SpecTemplate) != `{"胸径_cm":12}` {
		t.Errorf("expected SpecTemplate left unchanged by empty update input, got %q", string(d.SpecTemplate))
	}
}

// ---- DeleteUseCase ----

func TestDeleteUseCase_Execute_HappyPath(t *testing.T) {
	repo := &zzFakeRepo{}
	uc := apphort.NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.deleteCalled {
		t.Error("expected repo.Delete to be called")
	}
}

func TestDeleteUseCase_Execute_NotFoundMapped(t *testing.T) {
	repo := &zzFakeRepo{deleteErr: apphort.ErrNotFound}
	uc := apphort.NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, apphort.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteUseCase_Execute_OtherErrorWrapped(t *testing.T) {
	sentinel := errors.New("fk violation")
	repo := &zzFakeRepo{deleteErr: sentinel}
	uc := apphort.NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if errors.Is(err, apphort.ErrNotFound) {
		t.Error("non-not-found delete error must not be classified as ErrNotFound")
	}
	wantPrefix := "nursery dict delete:"
	if err == nil || err.Error()[:len(wantPrefix)] != wantPrefix {
		t.Errorf("expected error wrapped with %q, got %v", wantPrefix, err)
	}
}

// ---- RestoreUseCase ----

func TestRestoreUseCase_Execute_HappyPathReturnsEntry(t *testing.T) {
	want := &domain.NurseryDict{ID: uuid.New(), Name: "复原名"}
	repo := &zzFakeRepo{restoreVal: want}
	uc := apphort.NewRestoreUseCase(repo)

	got, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Error("expected same entry pointer returned from restore")
	}
}

func TestRestoreUseCase_Execute_NotFoundMapped(t *testing.T) {
	repo := &zzFakeRepo{restoreErr: apphort.ErrNotFound}
	uc := apphort.NewRestoreUseCase(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, apphort.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRestoreUseCase_Execute_OtherErrorWrapped(t *testing.T) {
	sentinel := errors.New("no soft-deleted row")
	repo := &zzFakeRepo{restoreErr: sentinel}
	uc := apphort.NewRestoreUseCase(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if errors.Is(err, apphort.ErrNotFound) {
		t.Error("non-not-found restore error must not be classified as ErrNotFound")
	}
	wantPrefix := "nursery dict restore:"
	if err == nil || err.Error()[:len(wantPrefix)] != wantPrefix {
		t.Errorf("expected error wrapped with %q, got %v", wantPrefix, err)
	}
}

// ---- Concurrency safety (run with -race) ----

func TestListUseCase_Execute_ConcurrentCallsAreRaceFree(t *testing.T) {
	repo := &zzFakeRepo{}
	uc := apphort.NewListUseCase(repo)

	const n = 20
	done := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(limit int) {
			_, _, err := uc.Execute(context.Background(), domain.ListFilter{
				TenantID: uuid.New(),
				Limit:    limit,
			})
			done <- err
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-done; err != nil {
			t.Errorf("unexpected error from concurrent Execute: %v", err)
		}
	}
}
