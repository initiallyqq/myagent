package mcp

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"myagent/internal/llm"
)

// Server exposes MCP-compatible HTTP endpoints so external LLM clients
// can discover and invoke tools via standard JSON-RPC style calls.
//
// Endpoints:
//   GET  /mcp/tools/list  → list all available tools
//   POST /mcp/tools/call  → call a tool by name with arguments
type Server struct {
	dispatcher ToolDispatcher
}

// ToolDispatcher is implemented by the skill registry.
type ToolDispatcher interface {
	Dispatch(ctx *gin.Context, name string, args json.RawMessage) (any, error)
}

func NewServer(d ToolDispatcher) *Server {
	return &Server{dispatcher: d}
}

// RegisterRoutes mounts MCP endpoints onto a Gin router group.
func (s *Server) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/tools/list", s.listTools)
	rg.POST("/tools/call", s.callTool)
}

// listTools returns all registered tool definitions (MCP tools/list).
func (s *Server) listTools(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"tools": ToolDefs(),
	})
}

// callTool executes a tool by name (MCP tools/call).
func (s *Server) callTool(c *gin.Context) {
	var req struct {
		Name      string          `json:"name"      binding:"required"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := s.dispatcher.Dispatch(c, req.Name, req.Arguments)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": result})
}

// ToolCallRequest is a convenience type for decoding LLM tool_call arguments.
func ParseArgs[T any](raw llm.ToolCall) (T, error) {
	var v T
	err := json.Unmarshal([]byte(raw.Function.Arguments), &v)
	return v, err
}
