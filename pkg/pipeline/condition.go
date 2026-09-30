package pipeline

import (
	"fmt"
	"regexp"
	"strings"
)

// ConditionContext supplies execution state for evaluating job 'if' conditions.
type ConditionContext struct {
	UpstreamStatuses map[string]string // Status of upstream dependency jobs
	Env              map[string]string // Current environment variables
}

var (
	envEqRegex  = regexp.MustCompile(`^(?:\$\{\{\s*)?env\.([a-zA-Z0-9_]+)\s*==\s*['"]([^'"]*)['"](?:\s*\}\})?$`)
	envNeqRegex = regexp.MustCompile(`^(?:\$\{\{\s*)?env\.([a-zA-Z0-9_]+)\s*!=\s*['"]([^'"]*)['"](?:\s*\}\})?$`)
)

// EvaluateCondition evaluates whether a job should be executed based on its 'if' expression.
func EvaluateCondition(expr string, ctx ConditionContext) (bool, error) {
	expr = strings.TrimSpace(expr)

	// Strip ${{ ... }} wrapper if present
	if strings.HasPrefix(expr, "${{") && strings.HasSuffix(expr, "}}") {
		expr = strings.TrimSpace(expr[3 : len(expr)-2])
	}

	// Default condition when empty is success()
	if expr == "" || expr == "success()" {
		return isSuccess(ctx.UpstreamStatuses), nil
	}

	if expr == "always()" {
		return true, nil
	}

	if expr == "failure()" {
		return isFailure(ctx.UpstreamStatuses), nil
	}

	// Check env.KEY == 'val'
	if matches := envEqRegex.FindStringSubmatch(expr); len(matches) == 3 {
		key := matches[1]
		expectedVal := matches[2]
		actualVal := ctx.Env[key]
		return actualVal == expectedVal, nil
	}

	// Check env.KEY != 'val'
	if matches := envNeqRegex.FindStringSubmatch(expr); len(matches) == 3 {
		key := matches[1]
		expectedVal := matches[2]
		actualVal := ctx.Env[key]
		return actualVal != expectedVal, nil
	}

	// Check simple boolean literals
	if strings.EqualFold(expr, "true") {
		return true, nil
	}
	if strings.EqualFold(expr, "false") {
		return false, nil
	}

	return false, fmt.Errorf("unsupported condition expression: '%s'", expr)
}

func isSuccess(statuses map[string]string) bool {
	if len(statuses) == 0 {
		return true
	}
	for _, status := range statuses {
		if status != "PASSED" {
			return false
		}
	}
	return true
}

func isFailure(statuses map[string]string) bool {
	if len(statuses) == 0 {
		return false
	}
	for _, status := range statuses {
		if status == "FAILED" {
			return true
		}
	}
	return false
}
