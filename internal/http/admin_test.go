package http

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go.orx.me/xbot/internal/conf"
)

func TestAdminAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", adminAuth(), func(c *gin.Context) { c.Status(nethttp.StatusNoContent) })

	do := func(header string) int {
		req := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	old := conf.Conf.AdminToken
	t.Cleanup(func() { conf.Conf.AdminToken = old })

	conf.Conf.AdminToken = ""
	if got := do("Bearer anything"); got != nethttp.StatusServiceUnavailable {
		t.Fatalf("disabled: got %d", got)
	}

	conf.Conf.AdminToken = "secret"
	if got := do(""); got != nethttp.StatusUnauthorized {
		t.Fatalf("missing: got %d", got)
	}
	if got := do("Bearer wrong"); got != nethttp.StatusUnauthorized {
		t.Fatalf("wrong: got %d", got)
	}
	if got := do("Bearer secret"); got != nethttp.StatusNoContent {
		t.Fatalf("ok: got %d", got)
	}
}
