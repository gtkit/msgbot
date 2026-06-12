package ginews

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	news "github.com/gtkit/msgbot"
)

func TestMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mgr := news.NewManager(&ginProvider{})

	r := gin.New()
	r.Use(Middleware(mgr))
	r.GET("/", func(c *gin.Context) {
		if From(c) != mgr {
			t.Fatal("manager mismatch")
		}
		if ProviderFrom(c, news.PlatformFeishu) == nil {
			t.Fatal("provider missing")
		}
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", w.Code)
	}
}

func TestMustFromPanics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = MustFrom(c)
}

type ginProvider struct{}

func (*ginProvider) SendText(context.Context, string, ...news.SendOption) error {
	return nil
}

func (*ginProvider) SendMarkdown(context.Context, string, string, ...news.SendOption) error {
	return nil
}

func (*ginProvider) SendRichText(context.Context, *news.RichTextMessage) error {
	return nil
}

func (*ginProvider) SendImage(context.Context, *news.ImageMessage) error {
	return nil
}

func (*ginProvider) Platform() news.Platform {
	return news.PlatformFeishu
}
