package intake

import (
	"context"
	"io"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
)

// List returns open and recent rows. own limits the list to that member.
func (s *Service) List(ctx context.Context, tenant uuid.UUID, own *uuid.UUID, limit, offset int) ([]*domain.Document, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.Repo.List(ctx, tenant, own, limit, offset)
}

// CountOpen is the sidebar badge: rows still waiting for a person.
func (s *Service) CountOpen(ctx context.Context, tenant uuid.UUID, own *uuid.UUID) (int, error) {
	return s.Repo.CountOpen(ctx, tenant, own)
}

// Get loads one row and the open bills for the partner the reviewer is looking at.
// partner and direction override the stored suggestion so the desk can switch them.
func (s *Service) Get(ctx context.Context, tenant, id uuid.UUID, own, partner *uuid.UUID, direction string, choosePartner bool) (*domain.Document, error) {
	doc, err := s.Repo.Get(ctx, tenant, id)
	if err != nil {
		return nil, err
	}
	if own != nil && doc.CreatedBy != *own {
		return nil, ErrNotFound
	}
	pid := uuid.Nil
	if choosePartner {
		if partner != nil {
			pid = *partner
		}
	} else if doc.Parsed.PartnerID != "" {
		pid, _ = uuid.Parse(doc.Parsed.PartnerID)
	}
	dir := doc.Direction
	if direction == domain.DirectionIn || direction == domain.DirectionOut {
		dir = direction
	}
	doc.OpenBills = nil
	if pid != uuid.Nil {
		bills, err := s.Repo.OpenBills(ctx, tenant, pid, dir)
		if err != nil {
			return nil, err
		}
		doc.OpenBills = bills
	}
	return doc, nil
}

// OpenFile returns the original bytes for a member who may see the row.
func (s *Service) OpenFile(ctx context.Context, tenant, id uuid.UUID, own *uuid.UUID) (io.ReadCloser, string, error) {
	doc, err := s.Get(ctx, tenant, id, own, nil, "", false)
	if err != nil {
		return nil, "", err
	}
	if doc.ObjectKey == "" || s.Blobs == nil {
		return nil, "", ErrNotFound
	}
	rc, ct, err := s.Blobs.Open(ctx, doc.ObjectKey)
	if err != nil {
		return nil, "", err
	}
	if ct == "" {
		ct = doc.ContentType
	}
	return rc, ct, nil
}
