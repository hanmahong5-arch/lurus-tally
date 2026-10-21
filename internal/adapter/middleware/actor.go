package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

// ActorUUID maps the OIDC subject to a stable creator UUID (see
// team.ActorIDFromSubject). Empty subject (PAT path, no AuthMiddleware)
// returns uuid.Nil.
func ActorUUID(c *gin.Context) uuid.UUID {
	return team.ActorIDFromSubject(GetIDPSubject(c))
}
