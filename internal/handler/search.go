package handler

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"myagent/internal/model"
	"myagent/internal/service"
	pkgtmpl "myagent/pkg/template"
	"myagent/pkg/sse"
)

// SearchRequest is the HTTP body for the main search endpoint.
type SearchRequest struct {
	Query string `json:"query" binding:"required,min=2,max=500"`
}

// SearchHandler handles POST /api/v1/search with SSE streaming.
type SearchHandler struct {
	intentSvc *service.IntentService
	searchSvc *service.SearchService
	cacheSvc  *service.CacheService
	userRepo  interface {
		GetByOpenID(ctx interface{}, openid string) (*model.User, error)
	}
}

func NewSearchHandler(
	intentSvc *service.IntentService,
	searchSvc *service.SearchService,
	cacheSvc *service.CacheService,
) *SearchHandler {
	return &SearchHandler{
		intentSvc: intentSvc,
		searchSvc: searchSvc,
		cacheSvc:  cacheSvc,
	}
}

// Handle is the Gin handler function.
func (h *SearchHandler) Handle(c *gin.Context) {
	var req SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数无效: " + err.Error()})
		return
	}

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	sw := sse.New(c.Writer, func() { c.Writer.Flush() })
	ctx := c.Request.Context()

	// ── 1. Cache check ─────────────────────────────────────────────────────
	cacheKey := h.cacheSvc.CacheKey(req.Query)
	var cached pkgtmpl.SearchReply
	if err := h.cacheSvc.Get(ctx, cacheKey, &cached); err == nil && cached.Done {
		slog.Info("search: cache hit", "key", cacheKey)
		_ = sw.Send(cached)
		sw.Done()
		return
	}

	// ── 2. Intent extraction ────────────────────────────────────────────────
	intent, fromFallback, err := h.intentSvc.Extract(ctx, req.Query)
	if err != nil {
		slog.Error("search: intent extraction failed", "err", err)
		_ = sw.SendText(`{"error":"无法理解您的意图，请换个方式描述"}`)
		sw.Done()
		return
	}
	if fromFallback {
		slog.Info("search: used regex fallback", "intent", intent)
	}

	// ── 3. Generate embedding for vector search ─────────────────────────────
	embedding, embedErr := h.intentSvc.EmbedIntent(ctx, intent)
	if embedErr != nil {
		slog.Warn("search: embedding failed, proceeding without vector", "err", embedErr)
	}

	q := &model.SearchQuery{Intent: *intent, Embedding: embedding}

	// ── 4. Resolve requester user ID ────────────────────────────────────────
	var requesterID int64
	if openid := c.GetHeader("X-Openid"); openid != "" {
		// best-effort; ignore error if user not yet registered
		if u, lookupErr := lookupUserByOpenID(c, openid); lookupErr == nil {
			requesterID = u.ID
		}
	}

	// ── 5. Hybrid search + degradation ─────────────────────────────────────
	result, err := h.searchSvc.Search(ctx, q, requesterID)
	if err != nil {
		slog.Error("search: search failed", "err", err)
		_ = sw.SendText(`{"error":"搜索服务暂时不可用，请稍后重试"}`)
		sw.Done()
		return
	}

	// ── 6. Build reply using string templates ──────────────────────────────
	reply := pkgtmpl.BuildReply(result, req.Query)

	// ── 7. Write cache if we got real results ──────────────────────────────
	if !result.Suspended && len(result.Users) > 0 {
		if err := h.cacheSvc.Set(ctx, cacheKey, reply); err != nil {
			slog.Warn("search: cache write failed", "err", err)
		}
	}

	_ = sw.Send(reply)
	sw.Done()
}

// lookupUserByOpenID is a lightweight helper used only inside the handler
// to avoid circular dependency on UserRepo.
func lookupUserByOpenID(c *gin.Context, openid string) (*model.User, error) {
	// Retrieved from Gin context if set by auth middleware
	if raw, exists := c.Get("user"); exists {
		if u, ok := raw.(*model.User); ok {
			return u, nil
		}
	}
	return nil, sql.ErrNoRows
}
