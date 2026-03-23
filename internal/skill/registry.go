package skill

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
)

// Skill is the interface all skills must implement.
type Skill interface {
	// Name returns the tool name as registered in MCP tools.
	Name() string
	// Execute runs the skill with the given raw JSON arguments.
	// Returns a JSON-serialisable result or an error.
	Execute(ctx context.Context, args json.RawMessage) (any, error)
}

// Registry holds all registered skills and implements mcp.ToolDispatcher.
type Registry struct {
	skills map[string]Skill
}

func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]Skill)}
}

// Register adds a skill to the registry.
func (r *Registry) Register(s Skill) {
	r.skills[s.Name()] = s
}

// Get returns a skill by name.
func (r *Registry) Get(name string) (Skill, bool) {
	s, ok := r.skills[name]
	return s, ok
}

// Dispatch implements mcp.ToolDispatcher.
func (r *Registry) Dispatch(c *gin.Context, name string, args json.RawMessage) (any, error) {
	s, ok := r.skills[name]
	if !ok {
		return nil, fmt.Errorf("unknown skill: %s", name)
	}
	return s.Execute(c.Request.Context(), args)
}

// DispatchContext dispatches by context (used inside the agent loop, not over HTTP).
func (r *Registry) DispatchContext(ctx context.Context, name string, args json.RawMessage) (any, error) {
	s, ok := r.skills[name]
	if !ok {
		return nil, fmt.Errorf("unknown skill: %s", name)
	}
	return s.Execute(ctx, args)
}
