package template

import (
	"fmt"
	"strings"

	"myagent/internal/model"
	"myagent/internal/service"
)

// SearchReply builds a human-readable reply from search results using
// pure string templating. No LLM is involved.

type SearchReply struct {
	Message string          `json:"message"`
	Users   []*model.User   `json:"users,omitempty"`
	Done    bool            `json:"done"`
}

// BuildReply constructs the SSE response payload from the search result.
func BuildReply(result *service.SearchResult, query string) *SearchReply {
	if result.Suspended {
		return &SearchReply{
			Message: "当前还没有完全匹配的旅伴，您已成为首位发起人！\n我们会在有合适的小伙伴加入时第一时间通知您，请记得允许订阅通知。",
			Done:    true,
		}
	}

	if len(result.Users) == 0 {
		return &SearchReply{
			Message: "暂时没有找到合适的旅伴，换个关键词试试？",
			Done:    true,
		}
	}

	prefix := buildPrefix(result, query)
	userDescs := buildUserDescriptions(result.Users)

	msg := fmt.Sprintf("%s\n\n%s", prefix, userDescs)
	return &SearchReply{
		Message: msg,
		Users:   result.Users,
		Done:    true,
	}
}

func buildPrefix(result *service.SearchResult, query string) string {
	count := len(result.Users)
	switch {
	case result.Region:
		return fmt.Sprintf("在您的目的地附近，为你找到 %d 位志同道合的旅伴（已扩大搜索范围）：", count)
	case result.Degraded:
		return fmt.Sprintf("稍微放宽了条件，为你找到 %d 位旅伴：", count)
	default:
		return fmt.Sprintf("为你精准匹配到 %d 位旅伴：", count)
	}
}

func buildUserDescriptions(users []*model.User) string {
	var sb strings.Builder
	for i, u := range users {
		sb.WriteString(fmt.Sprintf("%d. %s", i+1, formatUser(u)))
		if i < len(users)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func formatUser(u *model.User) string {
	parts := []string{u.Nickname}

	if mbti, ok := u.Tags["mbti"].(string); ok && mbti != "" {
		parts = append(parts, fmt.Sprintf("MBTI: %s", mbti))
	}

	if len(u.Destinations) > 0 {
		parts = append(parts, fmt.Sprintf("去向: %s", strings.Join(u.Destinations, "/")))
	}

	if u.BudgetMax > 0 && u.BudgetMax < 999999 {
		parts = append(parts, fmt.Sprintf("预算: %d 元", u.BudgetMax))
	}

	if hobbies, ok := u.Tags["hobby"].([]any); ok && len(hobbies) > 0 {
		hStrs := make([]string, 0, len(hobbies))
		for _, h := range hobbies {
			if hs, ok := h.(string); ok {
				hStrs = append(hStrs, hs)
			}
		}
		if len(hStrs) > 0 {
			parts = append(parts, fmt.Sprintf("兴趣: %s", strings.Join(hStrs, "、")))
		}
	}

	return strings.Join(parts, " | ")
}
