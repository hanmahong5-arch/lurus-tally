package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Item is one line of the operation log as the web shows it.
type Item struct {
	ID            uuid.UUID `json:"id"`
	At            time.Time `json:"at"`
	ActorName     string    `json:"actor_name"`
	Action        string    `json:"action"`
	ActionLabel   string    `json:"action_label"`
	Resource      string    `json:"resource"`
	ResourceLabel string    `json:"resource_label"`
	ResourceID    string    `json:"resource_id"`
	ResourceNo    string    `json:"resource_no"`
	Summary       string    `json:"summary"`
}

const (
	nameSystem  = "系统"
	nameRemoved = "已移出的成员"
)

// ListUseCase reads the operation log of one shop.
type ListUseCase struct {
	repo  Repo
	names MemberNamer
}

// NewListUseCase wires the use case. names may be nil (every actor then reads as removed/system).
func NewListUseCase(repo Repo, names MemberNamer) *ListUseCase {
	return &ListUseCase{repo: repo, names: names}
}

// Execute returns the page and the total count, newest first.
func (u *ListUseCase) Execute(ctx context.Context, tenantID uuid.UUID, f Filter) ([]Item, int, error) {
	rows, total, err := u.repo.List(ctx, tenantID, f)
	if err != nil {
		return nil, 0, err
	}
	var names map[uuid.UUID]string
	if u.names != nil && len(rows) > 0 {
		if names, err = u.names.MemberNames(ctx, tenantID); err != nil {
			return nil, 0, err
		}
	}
	items := make([]Item, 0, len(rows))
	for _, r := range rows {
		actor := nameSystem
		if r.UserID != uuid.Nil {
			actor = nameRemoved
			if n, ok := names[r.UserID]; ok {
				actor = n
			}
		}
		rid := ""
		if r.ResourceID != uuid.Nil {
			rid = r.ResourceID.String()
		}
		items = append(items, Item{
			ID: r.ID, At: r.CreatedAt, ActorName: actor,
			Action: r.Action, ActionLabel: ActionLabel(r.Action),
			Resource: r.Resource, ResourceLabel: ResourceLabel(r.Resource),
			ResourceID: rid, ResourceNo: r.ResourceNo, Summary: Summary(r.Changes),
		})
	}
	return items, total, nil
}
