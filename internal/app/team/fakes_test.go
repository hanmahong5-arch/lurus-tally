package team

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

type fakeStore struct {
	mu       sync.Mutex
	name     string
	orgID    int64
	tenantID uuid.UUID
	maps     map[string]*Mapping // by sub

	// Other shops (keyed by tenant id): their platform org, whether they hold only sample data.
	shopOrg    map[uuid.UUID]int64
	shopEmpty  map[uuid.UUID]bool
	abandoned  []uuid.UUID
	abandonErr error
	// abandonRetires: a concurrent abandon retired the caller's mapping first.
	abandonRetires bool
	// abandonWinner: the mapping the concurrent winner left behind after retiring the old one.
	abandonWinner *Mapping
	insertErr     error
	lookupMiss    map[string]bool // MappingBySub answers "none" once for these subs (simulates a race)
	unlinked      map[int64]bool  // orgs whose shop was abandoned (no live tenant)
	printName     string          // shop name saved in the print settings
	profileType   string          // shop business type ("" = unknown)
	profileErr    error
}

func newFakeStore(tenantID uuid.UUID) *fakeStore {
	return &fakeStore{tenantID: tenantID, name: "测试店", maps: map[string]*Mapping{}}
}

func (f *fakeStore) TenantOrg(_ context.Context, id uuid.UUID) (string, int64, error) {
	if id != f.tenantID {
		if org, ok := f.shopOrg[id]; ok {
			return "误开的店", org, nil
		}
		return "", 0, ErrTenantNotFound
	}
	return f.name, f.orgID, nil
}

func (f *fakeStore) PrintShopName(_ context.Context, _ uuid.UUID) (string, error) {
	return f.printName, nil
}

func (f *fakeStore) ProfileType(_ context.Context, _ uuid.UUID) (string, error) {
	return f.profileType, f.profileErr
}

func (f *fakeStore) SetTenantOrg(_ context.Context, _ uuid.UUID, org int64) error {
	f.orgID = org
	return nil
}

func (f *fakeStore) TenantByOrg(_ context.Context, org int64) (uuid.UUID, error) {
	if f.unlinked[org] {
		return uuid.Nil, nil
	}
	if org == f.orgID && org != 0 {
		return f.tenantID, nil
	}
	return uuid.Nil, nil
}

func (f *fakeStore) MappingBySub(_ context.Context, sub string) (*Mapping, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lookupMiss[sub] {
		delete(f.lookupMiss, sub)
		return nil, nil
	}
	if m, ok := f.maps[sub]; ok {
		c := *m
		return &c, nil
	}
	return nil, nil
}

func (f *fakeStore) MemberCount(_ context.Context, t uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.maps {
		if m.TenantID == t {
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) ShopIsEmpty(_ context.Context, t uuid.UUID) (bool, error) {
	return f.shopEmpty[t], nil
}

func (f *fakeStore) AbandonShop(_ context.Context, t uuid.UUID, sub string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.abandonErr != nil {
		if f.abandonRetires {
			delete(f.maps, sub)
			if f.abandonWinner != nil {
				w := *f.abandonWinner
				f.maps[sub] = &w
			}
		}
		return f.abandonErr
	}
	f.abandoned = append(f.abandoned, t)
	delete(f.maps, sub)
	return nil
}

func (f *fakeStore) MappingByAccount(_ context.Context, t uuid.UUID, acct int64) (*Mapping, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.maps {
		if m.TenantID == t && m.AccountID == acct {
			c := *m
			return &c, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) InsertMapping(_ context.Context, m Mapping) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertErr != nil {
		return f.insertErr
	}
	if _, ok := f.maps[m.IDPSubject]; ok {
		return ErrMappingExists
	}
	f.maps[m.IDPSubject] = &m
	return nil
}

func (f *fakeStore) SyncMapping(_ context.Context, _ uuid.UUID, sub, role string, acct int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.maps[sub]; ok {
		m.Role = role
		if acct > 0 {
			m.AccountID = acct
		}
	}
	return nil
}

func (f *fakeStore) DeleteMappingByAccount(_ context.Context, t uuid.UUID, acct int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sub, m := range f.maps {
		if m.TenantID == t && m.AccountID == acct {
			delete(f.maps, sub)
		}
	}
	return nil
}

type fakeAccounts struct{ ids map[string]int64 }

func (f fakeAccounts) EnsureAccount(_ context.Context, sub, _, _ string) (int64, error) {
	if id, ok := f.ids[sub]; ok {
		return id, nil
	}
	return 0, ErrUnavailable
}

type updateCall struct {
	acct, actor int64
	role        string
	pr          map[string]string
}

type fakeOrg struct {
	mu         sync.Mutex
	members    map[int64]*OrgMember
	getCalls   int
	getErr     error
	ensureCnt  int
	updates    []updateCall
	removed    []int64
	acceptCnt  int
	acceptRes  *AcceptResult
	acceptErr  error
	inviteRole string
	invitePR   map[string]string
	orgID      int64

	soloOrgID int64 // an extra org whose only member is soloMember
	solo      []OrgMember
	dissolved []int64
	listErr   error // ListMembers of the extra org fails
	invites   map[int64][]OrgInvite
	revoked   []uuid.UUID
}

func (f *fakeOrg) DissolveOrg(_ context.Context, orgID, _ int64) error {
	f.dissolved = append(f.dissolved, orgID)
	return nil
}

func newFakeOrg(orgID int64) *fakeOrg {
	return &fakeOrg{members: map[int64]*OrgMember{}, orgID: orgID}
}

func (f *fakeOrg) EnsureOrg(_ context.Context, _ string, owner int64, _, _ string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureCnt++
	if _, ok := f.members[owner]; !ok {
		f.members[owner] = &OrgMember{AccountID: owner, Role: "owner", ProductRoles: map[string]string{}}
	}
	return f.orgID, nil
}

func (f *fakeOrg) ListMembers(_ context.Context, orgID int64) ([]OrgMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.soloOrgID != 0 && orgID == f.soloOrgID {
		return f.solo, f.listErr
	}
	var out []OrgMember
	for _, m := range f.members {
		out = append(out, *m)
	}
	return out, nil
}

func (f *fakeOrg) GetMember(_ context.Context, _ int64, acct int64) (*OrgMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	m, ok := f.members[acct]
	if !ok {
		return nil, ErrNotMember
	}
	c := *m
	return &c, nil
}

func (f *fakeOrg) UpdateMember(_ context.Context, _ int64, acct, actor int64, role string, pr map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, updateCall{acct, actor, role, pr})
	if m, ok := f.members[acct]; ok {
		m.Role, m.ProductRoles = role, pr
	}
	return nil
}

func (f *fakeOrg) RemoveMember(_ context.Context, _ int64, acct, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, acct)
	delete(f.members, acct)
	return nil
}

func (f *fakeOrg) CreateInvite(_ context.Context, _ int64, _ int64, role string, pr map[string]string, _ int) (*InviteCreated, error) {
	f.inviteRole, f.invitePR = role, pr
	return &InviteCreated{InviteID: uuid.New(), Token: "tok123", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (f *fakeOrg) ListInvites(_ context.Context, orgID int64) ([]OrgInvite, error) {
	return f.invites[orgID], nil
}

func (f *fakeOrg) RevokeInvite(_ context.Context, _ int64, id uuid.UUID, _ int64) error {
	f.revoked = append(f.revoked, id)
	return nil
}

func (f *fakeOrg) AcceptInvite(_ context.Context, _ string, _ int64) (*AcceptResult, error) {
	f.acceptCnt++
	return f.acceptRes, f.acceptErr
}
