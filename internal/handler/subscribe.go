package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"myagent/internal/model"
	"myagent/internal/service"
)

// SubscribeHandler handles WeChat subscription authorization.
type SubscribeHandler struct {
	notifySvc *service.NotifyService
}

func NewSubscribeHandler(ns *service.NotifyService) *SubscribeHandler {
	return &SubscribeHandler{notifySvc: ns}
}

// Subscribe handles POST /api/v1/subscribe
func (h *SubscribeHandler) Subscribe(c *gin.Context) {
	var req model.SubscribeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Record the subscription preference in Redis or DB.
	// For now we acknowledge; actual storage happens via WeChat's server-side callback.
	c.JSON(http.StatusOK, gin.H{
		"message":     "订阅成功，有新旅伴时我们会第一时间通知您",
		"openid":      req.OpenID,
		"template_id": req.TemplateID,
	})
}
