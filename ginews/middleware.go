// Package ginews provides Gin middleware for integrating the news
// multi-platform messaging provider into HTTP handlers.
package ginews

import (
	"github.com/gin-gonic/gin"
	news "github.com/gtkit/msgbot"
)

const managerKey = "news.Manager"

// Middleware returns a Gin middleware that injects the news.Manager
// into the Gin context, making it accessible in downstream handlers.
func Middleware(mgr *news.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(managerKey, mgr)
		c.Next()
	}
}

// From retrieves the news.Manager from the Gin context.
// Returns nil if the middleware was not applied.
// This is the recommended way to access the Manager in handlers.
func From(c *gin.Context) *news.Manager {
	v, ok := c.Get(managerKey)
	if !ok {
		return nil
	}
	mgr, _ := v.(*news.Manager)
	return mgr
}

// MustFrom retrieves the news.Manager from the Gin context.
// It panics if the Manager is not found, indicating the Middleware was not registered.
//
// NOTE: Only use MustFrom in routes where Middleware is guaranteed to be registered.
// For safer usage, prefer From() with a nil check instead.
func MustFrom(c *gin.Context) *news.Manager {
	mgr := From(c)
	if mgr == nil {
		panic("ginews: Manager not found in context; register ginews.Middleware first")
	}
	return mgr
}

// ProviderFrom retrieves a specific platform's Provider from the Gin context.
// Returns nil if the platform is not registered or middleware was not applied.
func ProviderFrom(c *gin.Context, platform news.Platform) news.Provider {
	mgr := From(c)
	if mgr == nil {
		return nil
	}
	return mgr.Get(platform)
}
