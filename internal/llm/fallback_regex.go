package llm

import (
	"regexp"
	"strconv"
	"strings"

	"myagent/internal/model"
)

// destination keywords → canonical name
var destDict = map[string]string{
	"西藏": "西藏", "拉萨": "西藏", "布达拉": "西藏",
	"大西北": "大西北", "新疆": "新疆", "敦煌": "敦煌", "青海": "青海",
	"云南": "云南", "大理": "大理", "丽江": "丽江", "香格里拉": "云南",
	"四川": "四川", "成都": "四川", "稻城": "四川",
	"广西": "广西", "桂林": "广西",
	"海南": "海南", "三亚": "海南",
	"北京": "北京", "上海": "上海", "厦门": "厦门", "杭州": "杭州",
	"东南亚": "东南亚", "泰国": "泰国", "越南": "越南", "巴厘岛": "巴厘岛",
	"日本": "日本", "欧洲": "欧洲",
}

var (
	reGenderF       = regexp.MustCompile(`女|姐|妹|闺蜜|她`)
	reGenderM       = regexp.MustCompile(`男|哥|弟|兄弟|他`)
	reBudget        = regexp.MustCompile(`(\d+)\s*[块元千万]`)
	reMonth         = regexp.MustCompile(`(\d{1,2})\s*月`)
	rePersonality   = regexp.MustCompile(`[EI]人|内向|外向|佛系|话痨|宅|活泼|稳重|文艺|户外|摄影|美食|背包`)
)

// FallbackExtract extracts intent using regex/dictionary when LLM fails.
// It never returns nil; instead returns a best-effort Intent.
func FallbackExtract(input string) *model.Intent {
	intent := &model.Intent{}

	// destination
	for kw, canonical := range destDict {
		if strings.Contains(input, kw) {
			intent.Dest = canonical
			break
		}
	}

	// gender
	if reGenderF.MatchString(input) {
		intent.Gender = "F"
	} else if reGenderM.MatchString(input) {
		intent.Gender = "M"
	}

	// budget: take the largest number followed by budget unit
	for _, m := range reBudget.FindAllStringSubmatch(input, -1) {
		v, _ := strconv.Atoi(m[1])
		// handle "千" / "万" multipliers in the surrounding character
		multiplied := v
		if strings.Contains(m[0], "千") {
			multiplied = v * 1000
		} else if strings.Contains(m[0], "万") {
			multiplied = v * 10000
		}
		if multiplied > intent.Budget {
			intent.Budget = multiplied
		}
	}

	// month
	if m := reMonth.FindStringSubmatch(input); len(m) > 1 {
		month, _ := strconv.Atoi(m[1])
		if month >= 1 && month <= 12 {
			intent.AvailableMonth = month
		}
	}

	// personality keywords
	for _, kw := range rePersonality.FindAllString(input, -1) {
		intent.PersonalityKeywords = append(intent.PersonalityKeywords, kw)
	}

	return intent
}
