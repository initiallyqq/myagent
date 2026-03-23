package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/redis/go-redis/v9"
)

// CacheService manages semantic result caching in Redis.
type CacheService struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewCacheService(rdb *redis.Client, ttlSeconds int) *CacheService {
	return &CacheService{rdb: rdb, ttl: time.Duration(ttlSeconds) * time.Second}
}

// CacheKey produces a stable hash key from a normalized user query string.
func (c *CacheService) CacheKey(rawInput string) string {
	normalized := normalizeQuery(rawInput)
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("search:v1:%x", h[:8])
}

// Get retrieves a cached result. Returns nil,nil on cache miss.
func (c *CacheService) Get(ctx context.Context, key string, dest any) error {
	val, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil // miss
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(val, dest)
}

// Set stores a result in Redis with the configured TTL.
func (c *CacheService) Set(ctx context.Context, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, key, data, c.ttl).Err()
}

// normalizeQuery strips punctuation, folds spaces, to make semantically
// identical queries share the same cache key.
func normalizeQuery(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsPunct(r) || unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
