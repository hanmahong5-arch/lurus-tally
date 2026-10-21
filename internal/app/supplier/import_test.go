package supplier_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/text/encoding/simplifiedchinese"

	appsupp "github.com/hanmahong5-arch/lurus-tally/internal/app/supplier"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
)

func TestImport_GBKSkipsExistingName(t *testing.T) {
	repo := newFakeRepo()
	repo.listResult = []*domain.Supplier{{ID: uuid.New(), Name: "华东苗木场"}}
	repo.listTotal = 1
	data, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("供应商名称,联系人\n华东苗木场,王\n永固五金,赵\n新苗圃,钱\n"))
	uc := appsupp.NewImportUseCase(repo, appsupp.NewCreateUseCase(repo))
	res, err := uc.Execute(context.Background(), uuid.New(), data)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 2 || res.Skipped != 1 {
		t.Fatalf("res=%+v", res)
	}
}
