package mcp

import "myagent/internal/llm"

// ToolDefs returns the complete list of MCP tools exposed to the LLM agent.
// Each tool maps 1-to-1 with a Skill in the skill registry.
func ToolDefs() []llm.Tool {
	return []llm.Tool{
		{
			Type: "function",
			Function: llm.FuncSpec{
				Name:        "search_companions",
				Description: "搜索匹配的旅伴用户。根据目的地、性别、预算、性格关键词进行混合检索（精确过滤+向量相似度排序）。",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"dest": map[string]any{
							"type":        "string",
							"description": "目的地，如 '西藏'、'云南'",
						},
						"gender": map[string]any{
							"type":        "string",
							"enum":        []string{"M", "F", "X", ""},
							"description": "期望旅伴性别：M=男，F=女，X或空=不限",
						},
						"budget": map[string]any{
							"type":        "integer",
							"description": "用户预算上限（元），0 表示不限",
						},
						"personality_keywords": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
							"description": "性格或风格关键词，如 ['E人','户外','摄影']",
						},
						"available_month": map[string]any{
							"type":        "integer",
							"description": "出行月份 1-12，0 表示不限",
						},
					},
					"required": []string{"dest"},
				},
			},
		},
		{
			Type: "function",
			Function: llm.FuncSpec{
				Name:        "suspend_demand",
				Description: "当搜索无结果时，将用户需求挂入需求池，等待后续反向匹配并通知。",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"dest": map[string]any{
							"type": "string",
						},
						"gender": map[string]any{
							"type": "string",
						},
						"budget": map[string]any{
							"type": "integer",
						},
						"personality_keywords": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"available_month": map[string]any{
							"type": "integer",
						},
					},
					"required": []string{"dest"},
				},
			},
		},
		{
			Type: "function",
			Function: llm.FuncSpec{
				Name:        "save_memory",
				Description: "将对话中发现的用户偏好或重要信息保存到长期记忆，下次对话时会自动加载。",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"content": map[string]any{
							"type":        "string",
							"description": "要记住的一句话事实，如 '用户偏好户外徒步，不喜欢跟团'",
						},
					},
					"required": []string{"content"},
				},
			},
		},
		{
			Type: "function",
			Function: llm.FuncSpec{
				Name:        "get_user_profile",
				Description: "获取当前用户的画像信息（目的地偏好、标签、预算范围），用于个性化推荐。",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
					"required":   []string{},
				},
			},
		},
	}
}
