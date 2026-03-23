package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RateLimit returns a Gin middleware that enforces a sliding-window rate limit
// using Redis. Each user (identified by X-Openid header or client IP) is allowed
// at most maxRequests per 60-second window.
func RateLimit(rdb *redis.Client, maxRequests int) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := rateLimitKey(c)
		ctx := context.Background()

		now := time.Now().UnixMilli()
		windowMs := int64(60_000) // 1 minute
		cutoff := now - windowMs

		pipe := rdb.Pipeline()
		// Remove timestamps outside the window
		pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", cutoff))
		// Count remaining
		countCmd := pipe.ZCard(ctx, key)
		// Add current timestamp
		pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: now})
		// Reset TTL
		pipe.Expire(ctx, key, 70*time.Second)
		_, err := pipe.Exec(ctx)
		if err != nil {
			// Redis down → fail open (don't block the user)
			c.Next()
			return
		}

		if countCmd.Val() >= int64(maxRequests) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "请求过于频繁，请稍后再试",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func rateLimitKey(c *gin.Context) string {
	id := c.GetHeader("X-Openid")
	if id == "" {
		id = c.ClientIP()
	}
	return fmt.Sprintf("rl:v1:%s", id)
}
