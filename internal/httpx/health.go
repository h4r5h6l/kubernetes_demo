// Package httpx contains shared HTTP helpers and handlers.
package httpx

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Pinger is the minimal database behaviour needed by the readiness probe.
// pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Health is a liveness probe that intentionally does not touch the database.
// A temporary database outage should not cause the process to be restarted.
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Ready is a readiness probe: it verifies that the database is reachable.
// While it fails, an orchestrator should stop sending traffic to this instance,
// but must not restart it (that is what the liveness probe is for).
func Ready(db Pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unavailable",
				"error":  "database unreachable",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}
