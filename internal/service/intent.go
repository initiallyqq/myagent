package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"myagent/internal/llm"
	"myagent/internal/model"
)

// IntentService extracts structured intent from raw user input.
// It calls the LLM first; on timeout/error it falls back to regex.
type IntentService struct {
	llmClient *llm.Client
}

func NewIntentService(lc *llm.Client) *IntentService {
	return &IntentService{llmClient: lc}
}

// Extract returns a validated Intent. Never returns nil intent when err==nil.
func (s *IntentService) Extract(ctx context.Context, userInput string) (intent *model.Intent, fromFallback bool, err error) {
	// LLM path — already has 3s timeout set in http.Client
	llmCtx, cancel := context.WithTimeout(ctx, time.Duration(3)*time.Second)
	defer cancel()

	intent, err = s.llmClient.ExtractIntent(llmCtx, userInput)
	if err != nil {
		slog.Warn("intent: LLM failed, using regex fallback", "err", err)
		intent = llm.FallbackExtract(userInput)
		fromFallback = true
		err = nil
	}

	if validateErr := intent.Validate(); validateErr != nil {
		// last-resort: try regex on top of whatever we got
		fallback := llm.FallbackExtract(userInput)
		merged := mergeIntent(intent, fallback)
		if mergeErr := merged.Validate(); mergeErr != nil {
			return nil, true, fmt.Errorf("intent_empty: %w", mergeErr)
		}
		return merged, true, nil
	}
	return intent, fromFallback, nil
}

// EmbedIntent produces an embedding vector for the intent's personality keywords + dest.
func (s *IntentService) EmbedIntent(ctx context.Context, intent *model.Intent) ([]float32, error) {
	text := buildEmbedText(intent)
	embedCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.llmClient.EmbedText(embedCtx, text)
}

func buildEmbedText(intent *model.Intent) string {
	parts := []string{}
	if intent.Dest != "" {
		parts = append(parts, intent.Dest)
	}
	parts = append(parts, intent.PersonalityKeywords...)
	return strings.Join(parts, " ")
}

// mergeIntent fills zero-value fields in base from supplement.
func mergeIntent(base, supplement *model.Intent) *model.Intent {
	merged := *base
	if merged.Dest == "" {
		merged.Dest = supplement.Dest
	}
	if merged.Gender == "" {
		merged.Gender = supplement.Gender
	}
	if merged.Budget == 0 {
		merged.Budget = supplement.Budget
	}
	if merged.AvailableMonth == 0 {
		merged.AvailableMonth = supplement.AvailableMonth
	}
	if len(merged.PersonalityKeywords) == 0 {
		merged.PersonalityKeywords = supplement.PersonalityKeywords
	}
	return &merged
}
