package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"myagent/internal/llm"
	"myagent/internal/mcp"
	"myagent/internal/memory"
	pkgtmpl "myagent/pkg/template"
)

const maxReActSteps = 6

const systemPrompt = `你是一个旅伴匹配助手，帮助用户找到志同道合的旅行伙伴。

你有以下工具可以使用：
- search_companions: 根据意图搜索旅伴
- suspend_demand: 无结果时挂起需求，等待匹配通知
- save_memory: 保存用户偏好到长期记忆
- get_user_profile: 获取当前用户画像

工作流程：
1. 理解用户意图（目的地、性别偏好、预算、性格要求）
2. 调用 search_companions 搜索
3. 如果搜索有结果，直接返回
4. 如果无结果，调用 suspend_demand 挂起，告知用户
5. 如有值得记住的偏好，调用 save_memory

重要：最终回复必须简洁、口语化，不要输出 JSON，不要暴露工具调用细节。`

// OrchestratorInput contains all context needed for one agent run.
type OrchestratorInput struct {
	UserQuery   string
	UserID      int64
	OpenID      string
	MemoryCtx   string // pre-loaded memory block for prompt injection
}

// OrchestratorOutput is the final result of one agent run.
type OrchestratorOutput struct {
	Reply     *pkgtmpl.SearchReply
	RawText   string // final LLM text if no structured result
}

// Orchestrator runs the ReAct (Reason → Act → Observe) agent loop.
type Orchestrator struct {
	llmClient   *llm.Client
	skillReg    SkillRegistry
	memManager  *memory.Manager
}

// SkillRegistry is the interface the orchestrator uses to dispatch tool calls.
type SkillRegistry interface {
	DispatchContext(ctx context.Context, name string, args json.RawMessage) (any, error)
}

func NewOrchestrator(lc *llm.Client, sr SkillRegistry, mm *memory.Manager) *Orchestrator {
	return &Orchestrator{llmClient: lc, skillReg: sr, memManager: mm}
}

// Run executes the full ReAct loop for a single user request.
func (o *Orchestrator) Run(ctx context.Context, input OrchestratorInput) (*OrchestratorOutput, error) {
	// ── Build initial messages ─────────────────────────────────────────────
	sys := systemPrompt
	if input.MemoryCtx != "" {
		sys = sys + "\n\n" + input.MemoryCtx
	}

	messages := []llm.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: input.UserQuery},
	}

	tools := mcp.ToolDefs()
	var lastSearchResult *searchObservation

	// ── ReAct loop ─────────────────────────────────────────────────────────
	for step := 0; step < maxReActSteps; step++ {
		reply, err := o.llmClient.Chat(ctx, messages, tools)
		if err != nil {
			return nil, fmt.Errorf("orchestrator llm call step %d: %w", step, err)
		}

		// No tool calls → final answer
		if len(reply.ToolCalls) == 0 {
			out := &OrchestratorOutput{RawText: reply.Content}
			if lastSearchResult != nil {
				out.Reply = buildReplyFromObservation(lastSearchResult, input.UserQuery)
			}
			// Async: extract and save new memories from this turn
			go o.memManager.ExtractAndSave(
				context.Background(), input.UserID,
				input.UserQuery, reply.Content,
			)
			return out, nil
		}

		// Append assistant message with tool_calls
		messages = append(messages, llm.Message{
			Role:      "assistant",
			Content:   reply.Content,
			ToolCalls: reply.ToolCalls,
		})

		// ── Execute each tool call (Observe) ──────────────────────────────
		for _, tc := range reply.ToolCalls {
			slog.Info("agent: tool call", "step", step, "tool", tc.Function.Name)

			result, dispatchErr := o.skillReg.DispatchContext(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			var obsContent string
			if dispatchErr != nil {
				obsContent = fmt.Sprintf(`{"error":"%s"}`, dispatchErr.Error())
			} else {
				data, _ := json.Marshal(result)
				obsContent = string(data)
				// capture search result for structured reply building
				if tc.Function.Name == "search_companions" {
					var obs searchObservation
					_ = json.Unmarshal(data, &obs)
					lastSearchResult = &obs
				}
			}

			messages = append(messages, llm.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    obsContent,
			})
		}
	}

	// Exceeded max steps — return last observation as best effort
	out := &OrchestratorOutput{RawText: "很抱歉，处理您的请求时遇到了一些问题，请稍后重试。"}
	if lastSearchResult != nil {
		out.Reply = buildReplyFromObservation(lastSearchResult, input.UserQuery)
	}
	return out, nil
}

// ──────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────

type searchObservation struct {
	Found     bool              `json:"found"`
	Count     int               `json:"count"`
	Users     json.RawMessage   `json:"users"`
	Degraded  bool              `json:"degraded"`
	Region    bool              `json:"region"`
	Suspended bool              `json:"suspended"`
	DemandID  int64             `json:"demand_id"`
}

func buildReplyFromObservation(obs *searchObservation, query string) *pkgtmpl.SearchReply {
	if obs.Suspended || !obs.Found {
		msg := "当前还没有完全匹配的旅伴，您已成为首位发起人！我们会在有合适的小伙伴加入时第一时间通知您。"
		if !obs.Suspended {
			msg = "暂时没有找到合适的旅伴，换个关键词试试？"
		}
		return &pkgtmpl.SearchReply{Message: msg, Done: true}
	}

	prefix := buildPrefix(obs)
	return &pkgtmpl.SearchReply{
		Message: prefix,
		Done:    true,
	}
}

func buildPrefix(obs *searchObservation) string {
	switch {
	case obs.Region:
		return fmt.Sprintf("在您目的地附近，为你找到 %d 位志同道合的旅伴（已扩大搜索范围）", obs.Count)
	case obs.Degraded:
		return fmt.Sprintf("稍微放宽了条件，为你找到 %d 位旅伴", obs.Count)
	default:
		return fmt.Sprintf("为你精准匹配到 %d 位旅伴", obs.Count)
	}
}

func init() {
	_ = strings.TrimSpace // ensure strings is used
}
