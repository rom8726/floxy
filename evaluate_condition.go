package floxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"text/template"
)

var (
	templateCache = make(map[string]*template.Template)
	templateMutex sync.RWMutex
)

func evaluateCondition(expr string, stepCtx StepContext) (bool, error) {
	// Cache compiled templates for better performance and thread safety
	templateMutex.RLock()
	tpl, exists := templateCache[expr]
	templateMutex.RUnlock()

	if !exists {
		templateMutex.Lock()
		// Double-check after acquiring write lock
		tpl, exists = templateCache[expr]
		if !exists {
			var err error
			tpl, err = template.New("condition").Funcs(template.FuncMap{
				"eq": func(a, b any) bool { return compareEqual(a, b) },
				"ne": func(a, b any) bool { return !compareEqual(a, b) },
				"gt": func(a, b any) bool { return compareNumbers(a, b) > 0 },
				"lt": func(a, b any) bool { return compareNumbers(a, b) < 0 },
				"ge": func(a, b any) bool { return compareNumbers(a, b) >= 0 },
				"le": func(a, b any) bool { return compareNumbers(a, b) <= 0 },
				// Additional helper functions
				"contains":  func(s, substr string) bool { return strings.Contains(s, substr) },
				"hasPrefix": func(s, prefix string) bool { return strings.HasPrefix(s, prefix) },
				"hasSuffix": func(s, suffix string) bool { return strings.HasSuffix(s, suffix) },
			}).Parse(expr)
			if err != nil {
				templateMutex.Unlock()
				return false, fmt.Errorf("parse condition: %w", err)
			}
			// Limit cache size to prevent memory issues
			if len(templateCache) < 1000 {
				templateCache[expr] = tpl
			}
		}
		templateMutex.Unlock()
	}

	// Use thread-safe data cloning
	data := stepCtx.CloneData()
	if data == nil {
		data = make(map[string]any)
	}

	var buf bytes.Buffer
	// Execute template with cloned data to prevent concurrent access issues
	if err := tpl.Execute(&buf, data); err != nil {
		return false, fmt.Errorf("execute condition: %w", err)
	}

	result := strings.TrimSpace(buf.String())
	switch result {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		// Try to parse as boolean for more flexibility
		if b, err := strconv.ParseBool(result); err == nil {
			return b, nil
		}

		return false, fmt.Errorf("invalid condition output: %q", result)
	}
}

// compareEqual compares two values for equality with better type handling
func compareEqual(a, b any) bool {
	// Handle nil cases
	if a == nil && b == nil {
		return true
	}
	if a == nil && b != nil {
		// nil compared to a number should check if b is zero
		return toFloat64(b) == 0
	}
	if a != nil && b == nil {
		// number compared to nil should check if a is zero
		return toFloat64(a) == 0
	}

	// Try numeric comparison for numeric types
	aFloat := toFloat64(a)
	bFloat := toFloat64(b)

	// If both can be converted to numbers, compare as numbers
	// Check if conversion was successful by comparing with original values
	aIsNumeric := isNumericType(a) || isNumericString(a)
	bIsNumeric := isNumericType(b) || isNumericString(b)

	if aIsNumeric && bIsNumeric {
		return aFloat == bFloat
	}

	// Try direct comparison
	if a == b {
		return true
	}

	// Convert to strings for comparison as fallback
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// isNumericType checks if a value is a numeric type
func isNumericType(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64, json.Number, bool:
		return true
	default:
		return false
	}
}

// isNumericString checks if a string can be parsed as a number
func isNumericString(v any) bool {
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		if _, err := strconv.ParseFloat(s, 64); err == nil {
			return true
		}
		if _, err := strconv.ParseInt(s, 10, 64); err == nil {
			return true
		}
	}

	return false
}

func compareNumbers(a, b any) int {
	valA := toFloat64(a)
	valB := toFloat64(b)

	if valA > valB {
		return 1
	} else if valA < valB {
		return -1
	}

	return 0
}

func toFloat64(v any) float64 {
	if v == nil {
		return 0
	}

	// Handle json.Number type (common in JSON parsing)
	switch val := v.(type) {
	case json.Number:
		if f, err := val.Float64(); err == nil {
			return f
		}
		return 0
	case string:
		// Try to parse string as number
		val = strings.TrimSpace(val)
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
		// Try parsing as int
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return float64(i)
		}
		return 0
	case int:
		return float64(val)
	case int8:
		return float64(val)
	case int16:
		return float64(val)
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	case uint:
		return float64(val)
	case uint8:
		return float64(val)
	case uint16:
		return float64(val)
	case uint32:
		return float64(val)
	case uint64:
		return float64(val)
	case float32:
		return float64(val)
	case float64:
		return val
	case bool:
		// Handle boolean as 0 or 1
		if val {
			return 1
		}
		return 0
	default:
		// Use reflection as fallback
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return float64(rv.Int())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return float64(rv.Uint())
		case reflect.Float32, reflect.Float64:
			return rv.Float()
		case reflect.String:
			s := rv.String()
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				return f
			}
		}
		// For non-numeric types, return 0
		return 0
	}
}
