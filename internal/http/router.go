package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"butterfly.orx.me/core/log"
	"go.orx.me/xbot/internal/bot"
)

func Router(m *gin.Engine) {
	m.Any("/v1/webhook", func(c *gin.Context) {
		serveWebhook(c, "")
	})
	m.Any("/v1/webhooks/:name", func(c *gin.Context) {
		serveWebhook(c, c.Param("name"))
	})
}

func serveWebhook(c *gin.Context, name string) {
	logger := log.FromContext(c.Request.Context())
	logger.Debug("new webhook request",
		"bot", name,
		"header", c.Request.Header,
		"method", c.Request.Method,
	)

	handler, ok := bot.WebhookHandler(name)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	handler.ServeHTTP(c.Writer, c.Request)
}
