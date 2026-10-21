package team

import "github.com/google/uuid"

// actorNamespace is the fixed namespace for deriving actor UUIDs from non-UUID
// IdP subjects. Never change it: derived creator ids already stored would
// stop matching their owners.
var actorNamespace = uuid.MustParse("6f1c2a54-3b7e-4d0a-9c85-2e4b7a91d3f6")

// ActorIDFromSubject maps an IdP subject to a stable creator UUID. A UUID
// subject is returned as is; any other subject (e.g. a numeric id) is hashed
// with a fixed namespace so the same person always gets the same id. An empty
// subject returns uuid.Nil.
func ActorIDFromSubject(sub string) uuid.UUID {
	if sub == "" {
		return uuid.Nil
	}
	if id, err := uuid.Parse(sub); err == nil {
		return id
	}
	return uuid.NewSHA1(actorNamespace, []byte("idp:"+sub))
}
