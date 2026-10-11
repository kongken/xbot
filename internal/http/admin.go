package http

import (
	"crypto/subtle"
	"errors"
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go.orx.me/xbot/internal/bot"
	"go.orx.me/xbot/internal/conf"
	"go.orx.me/xbot/internal/dao"
)

func adminRouter(m *gin.Engine) {
	g := m.Group("/v1/admin", adminAuth())
	g.GET("/bots", listBots)
	g.POST("/bots", createBot)
	g.GET("/bots/:name", getBot)
	g.PUT("/bots/:name", updateBot)
	g.DELETE("/bots/:name", deleteBot)
}

// adminAuth protects admin APIs with a static bearer token. The APIs are
// disabled when conf.AdminToken is empty.
func adminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		expected := conf.Conf.AdminToken
		if expected == "" {
			c.AbortWithStatusJSON(nethttp.StatusServiceUnavailable, gin.H{"error": "admin api disabled, set adminToken in config"})
			return
		}
		got, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			c.AbortWithStatusJSON(nethttp.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

type createBotRequest struct {
	Name   string `json:"name"`
	Token  string `json:"token"`
	Enable *bool  `json:"enable"`
}

type updateBotRequest struct {
	Token  *string `json:"token"`
	Enable *bool   `json:"enable"`
}

func listBots(c *gin.Context) {
	bots, err := bot.GetManager().List(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(nethttp.StatusOK, gin.H{"bots": bots})
}

func getBot(c *gin.Context) {
	info, err := bot.GetManager().Get(c.Request.Context(), c.Param("name"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(nethttp.StatusOK, info)
}

func createBot(c *gin.Context) {
	var req createBotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(nethttp.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enable := true
	if req.Enable != nil {
		enable = *req.Enable
	}
	info, err := bot.GetManager().Create(c.Request.Context(), req.Name, req.Token, enable)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(nethttp.StatusCreated, info)
}

func updateBot(c *gin.Context) {
	var req updateBotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(nethttp.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	info, err := bot.GetManager().Update(c.Request.Context(), c.Param("name"), req.Token, req.Enable)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(nethttp.StatusOK, info)
}

func deleteBot(c *gin.Context) {
	if err := bot.GetManager().Delete(c.Request.Context(), c.Param("name")); err != nil {
		respondError(c, err)
		return
	}
	c.Status(nethttp.StatusNoContent)
}

func respondError(c *gin.Context, err error) {
	status := nethttp.StatusInternalServerError
	switch {
	case errors.Is(err, dao.ErrBotNotFound):
		status = nethttp.StatusNotFound
	case errors.Is(err, dao.ErrBotExists):
		status = nethttp.StatusConflict
	case errors.Is(err, bot.ErrInvalidName), errors.Is(err, bot.ErrInvalidToken), errors.Is(err, bot.ErrStartFailed):
		status = nethttp.StatusBadRequest
	case errors.Is(err, bot.ErrReadOnly):
		status = nethttp.StatusForbidden
	}
	c.JSON(status, gin.H{"error": err.Error()})
}
