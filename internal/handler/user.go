package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"myagent/internal/llm"
	"myagent/internal/model"
	"myagent/internal/repo"
)

// UserHandler handles user registration and profile updates.
type UserHandler struct {
	userRepo  *repo.UserRepo
	llmClient *llm.Client
}

func NewUserHandler(ur *repo.UserRepo, lc *llm.Client) *UserHandler {
	return &UserHandler{userRepo: ur, llmClient: lc}
}

// Register handles POST /api/v1/user/register
func (h *UserHandler) Register(c *gin.Context) {
	var req model.UserRegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	// Generate embedding from the bio field
	var embedding []float32
	if req.Bio != "" {
		vec, err := h.llmClient.EmbedText(ctx, req.Bio)
		if err != nil {
			slog.Warn("user register: embed failed, storing without vector", "err", err)
		} else {
			embedding = vec
		}
	}

	id, err := h.userRepo.Upsert(ctx, &req, embedding)
	if err != nil {
		slog.Error("user register: upsert failed", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注册失败，请重试"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": id, "message": "注册/更新成功"})
}
