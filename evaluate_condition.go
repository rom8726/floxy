package floxy

import (
	"bytes"
	"fmt"
	"text/template"
)

func evaluateCondition(expr string, stepCtx StepContext) (bool, error) {
	tpl, err := template.New("condition").Funcs(template.FuncMap{
		"eq": func(a, b any) bool { return a == b },
		"ne": func(a, b any) bool { return a != b },
		"gt": func(a, b float64) bool { return a > b },
		"lt": func(a, b float64) bool { return a < b },
		"ge": func(a, b float64) bool { return a >= b },
		"le": func(a, b float64) bool { return a <= b },
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
