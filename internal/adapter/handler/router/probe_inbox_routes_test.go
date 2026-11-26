package router

import (
	"github.com/gin-gonic/gin"

	handlerintake "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/intake"
	handlerprint "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/print"
	appprint "github.com/hanmahong5-arch/lurus-tally/internal/app/print"
)

func probeInboxRoutes(rg *gin.RouterGroup) {
	handlerprint.New(appprint.New(nil, nil, nil, nil)).RegisterRoutes(rg)
	handlerintake.New(nil).RegisterRoutes(rg)
}
