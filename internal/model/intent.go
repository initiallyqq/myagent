package model

import (
	"errors"
	"strings"
)

// Intent is the structured output extracted from user natural language input.
type Intent struct {
	Dest                 string   `json:"dest"`
	Budget               int      `json:"budget"`
	Gender               string   `json:"gender"`                // M / F / X / "" (any)
	PersonalityKeywords  []string `json:"personality_keywords"`
	AvailableMonth       int      `json:"available_month"`        // 0 = unspecified
}

// Validate performs hard-type validation after JSON unmarshal.
// Returns an error describing the first violated constraint.
func (i *Intent) Validate() error {
	i.Dest = strings.TrimSpace(i.Dest)
	i.Gender = strings.ToUpper(strings.TrimSpace(i.Gender))

	if i.Gender != "" && i.Gender != "M" && i.Gender != "F" && i.Gender != "X" {
		i.Gender = "" // treat unknown gender as "any"
	}
	if i.Budget < 0 {
		i.Budget = 0
	}
	if i.AvailableMonth < 0 || i.AvailableMonth > 12 {
		i.AvailableMonth = 0
	}
	if i.Dest == "" && len(i.PersonalityKeywords) == 0 {
		return errors.New("intent has no dest and no personality keywords; cannot search")
	}
	return nil
}

// SearchQuery is built from Intent and used by the search service.
type SearchQuery struct {
	Intent
	Embedding  []float32 // vector produced from personality keywords
	Relaxed    bool      // true when running fallback (budget/time stripped)
	RegionOnly bool      // true when dest expanded to region
}
