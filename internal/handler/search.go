package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"myagent/internal/agent"
	"myagent/internal/memory"
	"myagent/internal/service"
	"myagent/pkg/sse"
	pkgtmpl "myagent/pkg/template"
)

// SearchRequest is the HTTP body for the main search endpoint.
type SearchRequest struct {
	Query string `json:"query" binding:"required,min=2,max=500"`
}

// SearchHandler handles POST /api/v1/search with SSE streaming.
// It delegates to the Agent Orchestrator which runs the ReAct loop
// using Function Calling, mem0 memory injection, and the Skill registry.
type SearchHandler struct {
	orchestrator *agent.Orchestrator
	cacheSvc     *service.CacheService
	memManager   *memory.Manager
}

func NewSearchHandler(
	orch *agent.Orchestrator,
	cacheSvc *service.CacheService,
	memMgr *memory.Manager,
) *SearchHandler {
	return &SearchHandler{
		orchestrator: orch,
		cacheSvc:     cacheSvc,
		memManager:   memMgr,
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

	// ── 2. Resolve user identity ────────────────────────────────────────────
	var userID int64
	openID := c.GetHeader("X-Openid")

	// ── 3. Load relevant memories (mem0) ────────────────────────────────────
	memCtx := h.memManager.RetrieveContext(ctx, userID, req.Query)

	// ── 4. Run Agent ReAct loop ─────────────────────────────────────────────
	output, err := h.orchestrator.Run(ctx, agent.OrchestratorInput{
		UserQuery: req.Query,
		UserID:    userID,
		OpenID:    openID,
		MemoryCtx: memCtx,
	})
	if err != nil {
		slog.Error("search: orchestrator failed", "err", err)
		_ = sw.SendText(`{"error":"搜索服务暂时不可用，请稍后重试"}`)
		sw.Done()
		return
	}

	// ── 5. Build final reply ────────────────────────────────────────────────
	var reply *pkgtmpl.SearchReply
	if output.Reply != nil {
		reply = output.Reply
	} else {
		reply = &pkgtmpl.SearchReply{
			Message: output.RawText,
			Done:    true,
		}
	}

	// ── 6. Cache results ────────────────────────────────────────────────────
	if reply.Done && len(reply.Users) > 0 {
		if err := h.cacheSvc.Set(ctx, cacheKey, reply); err != nil {
			slog.Warn("search: cache write failed", "err", err)
		}
	}

	_ = sw.Send(reply)
	sw.Done()
}
