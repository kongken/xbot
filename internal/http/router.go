package http

import (
	nethttp "net/http"

	"github.com/gin-gonic/gin"

	"butterfly.orx.me/core/log"
	"go.orx.me/xbot/internal/bot"
)

func Router(m *gin.Engine) {
	// legacy webhook path, served by the default bot.
	m.Any("/v1/webhook", webhookHandler(bot.DefaultBotName))
	m.Any("/v1/webhook/:name", func(c *gin.Context) {
		webhookHandler(c.Param("name"))(c)
	})

	adminRouter(m)
}

func webhookHandler(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := log.FromContext(c.Request.Context())
		logger.Debug("new webhook request",
			"bot", name,
			"header", c.Request.Header,
			"method", c.Request.Method,
		)
		h, ok := bot.GetManager().Handler(name)
		if !ok {
			c.Status(nethttp.StatusNotFound)
			return
		}
		h.ServeHTTP(c.Writer, c.Request)
	}
}
