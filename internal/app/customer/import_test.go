package customer_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/text/encoding/simplifiedchinese"

	appcust "github.com/hanmahong5-arch/lurus-tally/internal/app/customer"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
)

func TestImport_GBKSkipsExistingName(t *testing.T) {
	repo := newFakeRepo()
	repo.listResult = []*domain.Customer{{ID: uuid.New(), Name: "老王五金"}}
	repo.listTotal = 1
	data, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("客户名称,电话\n张三,13800000000\n 老王五金 ,\n李四,0571-1234\n"))
	uc := appcust.NewImportUseCase(repo, appcust.NewCreateUseCase(repo))
	res, err := uc.Execute(context.Background(), uuid.New(), data)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 2 || res.Skipped != 1 || len(res.Errors) != 0 {
		t.Fatalf("res=%+v", res)
	}
}

func TestImport_SplitsNameAndPhoneSkips(t *testing.T) {
	repo := newFakeRepo()
	repo.listResult = []*domain.Customer{{ID: uuid.New(), Name: "老王五金"}}
	repo.listTotal = 1
	csv := "客户名称,电话\n张三,13800000000\n老王五金,\n赵六,13800000000\n"
	uc := appcust.NewImportUseCase(repo, appcust.NewCreateUseCase(repo))
	res, err := uc.Execute(context.Background(), uuid.New(), []byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 1 || res.Skipped != 2 || res.SkippedByName != 1 || res.SkippedByPhone != 1 {
		t.Fatalf("res=%+v", res)
	}
	var sawName, sawPhone bool
	for _, r := range res.SkippedRows {
		if r.Row == 3 && strings.Contains(r.Message, "名称") {
			sawName = true
		}
		if r.Row == 4 && strings.Contains(r.Message, "手机号") {
			sawPhone = true
		}
	}
	if !sawName || !sawPhone {
		t.Fatalf("rows=%+v", res.SkippedRows)
	}
}
