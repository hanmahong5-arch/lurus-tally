package erasure

import (
	"context"
	"errors"
	"testing"
)

type mockRepo struct {
	n      int
	err    error
	called bool
	got    int64
}

func (m *mockRepo) EraseByPlatformAccount(_ context.Context, accountID int64) (int, error) {
	m.called = true
	m.got = accountID
	return m.n, m.err
}

func TestService_Erase_RejectsInvalidAccount(t *testing.T) {
	for _, id := range []int64{0, -1} {
		repo := &mockRepo{n: 1}
		_, err := NewService(repo, nil).Erase(context.Background(), id)
		if err == nil {
			t.Fatalf("account_id %d: want error, got nil", id)
		}
		if repo.called {
			t.Errorf("account_id %d: repo must NOT be called on a rejected id", id)
		}
	}
}

func TestService_Erase_DelegatesAndReportsCount(t *testing.T) {
	repo := &mockRepo{n: 2}
	n, err := NewService(repo, nil).Erase(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2 {
		t.Errorf("tenants_affected = %d, want 2", n)
	}
	if repo.got != 42 {
		t.Errorf("repo got account_id %d, want 42", repo.got)
	}
}

func TestService_Erase_NoData_IsZeroNotError(t *testing.T) {
	n, err := NewService(&mockRepo{n: 0}, nil).Erase(context.Background(), 7)
	if err != nil {
		t.Fatalf("no-data replay must not error, got: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
}

func TestService_Erase_WrapsRepoError(t *testing.T) {
	sentinel := errors.New("boom")
	_, err := NewService(&mockRepo{err: sentinel}, nil).Erase(context.Background(), 9)
	if !errors.Is(err, sentinel) {
		t.Fatalf("want wrapped repo error, got: %v", err)
	}
}
