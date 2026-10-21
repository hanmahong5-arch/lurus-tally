package platform

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"

	appteam "github.com/hanmahong5-arch/lurus-tally/internal/app/team"
)

// orgMemberSubjects are the platform membership events that change who is in a
// shop or what they may do.
var orgMemberSubjects = map[string]struct{}{
	appteam.EventJoined:      {},
	appteam.EventRemoved:     {},
	appteam.EventRoleChanged: {},
}

// ParseOrgMemberEvent decodes a member event delivered on subject. The platform
// wraps payloads in its IdentityEvent envelope ({"payload": {...}}); a bare
// payload is accepted too. ok is false for other subjects or unusable bodies.
func ParseOrgMemberEvent(subject string, data []byte) (appteam.MemberEvent, bool) {
	if _, hit := orgMemberSubjects[subject]; !hit {
		return appteam.MemberEvent{}, false
	}
	var env struct {
		Payload json.RawMessage `json:"payload"`
		OrgID   int64           `json:"org_id"`
		Account int64           `json:"account_id"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return appteam.MemberEvent{}, false
	}
	orgID, accountID := env.OrgID, env.Account
	if len(env.Payload) > 0 {
		var p struct {
			OrgID     int64 `json:"org_id"`
			AccountID int64 `json:"account_id"`
		}
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return appteam.MemberEvent{}, false
		}
		orgID, accountID = p.OrgID, p.AccountID
	}
	if orgID <= 0 {
		return appteam.MemberEvent{}, false
	}
	return appteam.MemberEvent{Type: subject, OrgID: orgID, AccountID: accountID}, true
}

// SubscribeOrgMemberEvents drops cached roles the moment a membership event
// arrives. It is a plain (non-durable) subscription: missing an event while
// disconnected is covered by the 60s role cache TTL.
func SubscribeOrgMemberEvents(nc *nats.Conn, svc *appteam.Service, log *slog.Logger) (*nats.Subscription, error) {
	return nc.Subscribe("identity.org.>", func(m *nats.Msg) {
		ev, ok := ParseOrgMemberEvent(m.Subject, m.Data)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		svc.HandleMemberEvent(ctx, ev)
		log.Debug("team: member event applied", slog.String("subject", m.Subject))
	})
}
