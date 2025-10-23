package floxy

import (
	"bytes"
	"fmt"
	"reflect"
	"text/template"
)

func evaluateCondition(expr string, stepCtx StepContext) (bool, error) {
	tpl, err := template.New("condition").Funcs(template.FuncMap{
		"eq": func(a, b any) bool { return fmt.Sprint(a) == fmt.Sprint(b) },
		"ne": func(a, b any) bool { return fmt.Sprint(a) != fmt.Sprint(b) },
		"gt": func(a, b any) bool { return compareNumbers(a, b) > 0 },
		"lt": func(a, b any) bool { return compareNumbers(a, b) < 0 },
		"ge": func(a, b any) bool { return compareNumbers(a, b) >= 0 },
		"le": func(a, b any) bool { return compareNumbers(a, b) <= 0 },
	}).Parse(expr)
	if err != nil {
		return false, fmt.Errorf("parse condition: %w", err)
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, stepCtx.CloneData()); err != nil {
		return false, fmt.Errorf("execute condition: %w", err)
	}

	result := buf.String()
	switch result {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid condition output: %q", result)
	}
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

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint())
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	default:
		// For non-numeric types, return 0 (this will cause comparison to fail)
		return 0
	}
}
