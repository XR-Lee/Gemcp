package researchbench

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Tool results are read as plain JSON so the benchmark runs unchanged against
// any Gemcp version; a field an older server does not send simply reads as
// absent.

func field(value any, path ...string) any {
	for _, key := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[key]
	}
	return value
}

func text(value any, path ...string) string {
	switch typed := field(value, path...).(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	}
	return ""
}

func number(value any, path ...string) (float64, bool) {
	switch typed := field(value, path...).(type) {
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case int:
		return float64(typed), true
	}
	return 0, false
}

func flag(value any, path ...string) bool {
	typed, _ := field(value, path...).(bool)
	return typed
}

func objects(value any, path ...string) []map[string]any {
	items, _ := field(value, path...).([]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			result = append(result, object)
		}
	}
	return result
}

func textList(value any, path ...string) []string {
	items, _ := field(value, path...).([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if typed, ok := item.(string); ok {
			result = append(result, typed)
		}
	}
	return result
}

// formatMetric matches the console and plan-sync formatting: four
// significant digits without exponent notation.
func formatMetric(value float64) string {
	formatted := strconv.FormatFloat(value, 'g', 4, 64)
	if strings.ContainsAny(formatted, "eE") {
		formatted = strconv.FormatFloat(value, 'f', -1, 64)
	}
	return formatted
}

// metricSpellings are the ways a report might print value, so the scorer
// credits any faithful rendering, not only the current format.
func metricSpellings(value float64) []string {
	return []string{
		formatMetric(value),
		strconv.FormatFloat(value, 'f', -1, 64),
		strconv.FormatFloat(value, 'f', 3, 64),
		strconv.FormatFloat(value, 'f', 4, 64),
	}
}
