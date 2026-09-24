// Package tenant — publisher_adapter.go: thin shim adapting the adapternats
// typed publisher to the ProfileEventPublisher interface this app package
// declares. Keeps the app/tenant package free of a hard dependency on the
// concrete NATS payload struct.
package tenant

import (
	"context"

	adapternats "github.com/hanmahong5-arch/lurus-tally/internal/adapter/nats"
)

// natsPublisherShim adapts the wider adapternats.Publisher interface (which
// has many PublishX methods) down to the single-method ProfileEventPublisher
// the use case depends on. The shim re-shapes our app-layer payload into the
// transport-layer payload at exactly one point.
type natsPublisherShim struct {
	pub adapternats.Publisher
}

// NewNATSProfileEventPublisher wraps the adapternats.Publisher so it
// satisfies ProfileEventPublisher. Returns nil when pub is nil so callers
// can pass the result straight into NewChooseProfileUseCase.
func NewNATSProfileEventPublisher(pub adapternats.Publisher) ProfileEventPublisher {
	if pub == nil {
		return nil
	}
	return &natsPublisherShim{pub: pub}
}

func (s *natsPublisherShim) PublishTenantProfileChanged(ctx context.Context, tenantID string, payload ProfileChangedPayload) error {
	return s.pub.PublishTenantProfileChanged(ctx, tenantID, adapternats.TenantProfileChangedPayload{
		TenantID:        payload.TenantID,
		ProfileType:     payload.ProfileType,
		PreviousProfile: payload.PreviousProfile,
		InventoryMethod: payload.InventoryMethod,
		ChangedBy:       payload.ChangedBy,
	})
}
