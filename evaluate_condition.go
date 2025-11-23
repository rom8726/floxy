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

	"github.com/shopspring/decimal"
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
				"eq": func(a, b any) bool { return compareEqualDecimal(a, b) },
				"ne": func(a, b any) bool { return !compareEqualDecimal(a, b) },
				"gt": func(a, b any) bool { return compareNumbersDecimal(a, b) > 0 },
				"lt": func(a, b any) bool { return compareNumbersDecimal(a, b) < 0 },
				"ge": func(a, b any) bool { return compareNumbersDecimal(a, b) >= 0 },
				"le": func(a, b any) bool { return compareNumbersDecimal(a, b) <= 0 },
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

// compareEqualDecimal compares two values for equality using decimal for numeric precision
func compareEqualDecimal(a, b any) bool {
	// Handle nil cases
	if a == nil && b == nil {
		return true
	}
	if a == nil && b != nil {
		// nil compared to a number should check if b is zero
		bDec, err := toDecimal(b)
		return err == nil && bDec.IsZero()
	}
	if a != nil && b == nil {
		// number compared to nil should check if a is zero
		aDec, err := toDecimal(a)
		return err == nil && aDec.IsZero()
	}

	// Try to convert both to decimal for precise comparison
	aDec, aErr := toDecimal(a)
	bDec, bErr := toDecimal(b)

	// If both can be converted to decimal, compare as decimals
	if aErr == nil && bErr == nil {
		return aDec.Equal(bDec)
	}

	// If only one is numeric, they're not equal
	if (aErr == nil) != (bErr == nil) {
		return false
	}

	// Try direct comparison
	if a == b {
		return true
	}

	// Convert to strings for comparison as fallback
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// compareNumbersDecimal compares two numbers using decimal for precision
func compareNumbersDecimal(a, b any) int {
	aDec, aErr := toDecimal(a)
	bDec, bErr := toDecimal(b)

	// If either conversion fails, fallback to treating non-numeric as 0
	if aErr != nil {
		aDec = decimal.Zero
	}
	if bErr != nil {
		bDec = decimal.Zero
	}

	return aDec.Cmp(bDec)
}

// toDecimal converts various types to decimal.Decimal for precise arithmetic
func toDecimal(v any) (decimal.Decimal, error) {
	if v == nil {
		return decimal.Zero, nil
	}

	switch val := v.(type) {
	case decimal.Decimal:
		return val, nil
	case json.Number:
		return decimal.NewFromString(string(val))
	case string:
		val = strings.TrimSpace(val)
		if val == "" {
			return decimal.Zero, nil
		}
		return decimal.NewFromString(val)
	case int:
		return decimal.NewFromInt(int64(val)), nil
	case int8:
		return decimal.NewFromInt(int64(val)), nil
	case int16:
		return decimal.NewFromInt(int64(val)), nil
	case int32:
		return decimal.NewFromInt(int64(val)), nil
	case int64:
		return decimal.NewFromInt(val), nil
	case uint:
		return decimal.NewFromInt(int64(val)), nil
	case uint8:
		return decimal.NewFromInt(int64(val)), nil
	case uint16:
		return decimal.NewFromInt(int64(val)), nil
	case uint32:
		return decimal.NewFromInt(int64(val)), nil
	case uint64:
		return decimal.NewFromInt(int64(val)), nil
	case float32:
		return decimal.NewFromFloat32(val), nil
	case float64:
		return decimal.NewFromFloat(val), nil
	case bool:
		if val {
			return decimal.NewFromInt(1), nil
		}
		return decimal.Zero, nil
	default:
		// Use reflection as fallback
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return decimal.NewFromInt(rv.Int()), nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			// Handle potential overflow
			uintVal := rv.Uint()
			if uintVal > 9223372036854775807 {
				return decimal.NewFromString(fmt.Sprintf("%d", uintVal))
			}
			return decimal.NewFromInt(int64(uintVal)), nil
		case reflect.Float32, reflect.Float64:
			return decimal.NewFromFloat(rv.Float()), nil
		case reflect.String:
			s := strings.TrimSpace(rv.String())
			if s == "" {
				return decimal.Zero, nil
			}
			return decimal.NewFromString(s)
		case reflect.Bool:
			if rv.Bool() {
				return decimal.NewFromInt(1), nil
			}
			return decimal.Zero, nil
		default:
			return decimal.Zero, fmt.Errorf("cannot convert %T to decimal", v)
		}
	}
}

func compareNumbers(a, b any) int {
	return compareNumbersDecimal(a, b)
}

func toFloat64(v any) float64 {
	dec, err := toDecimal(v)
	if err != nil {
		return 0
	}

	f, _ := dec.Float64()

	return f
}
