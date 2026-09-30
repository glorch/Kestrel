package pipeline

import (
	"testing"
)

func TestEvaluateCondition(t *testing.T) {
	ctxAllPassed := ConditionContext{
		UpstreamStatuses: map[string]string{"jobA": "PASSED", "jobB": "PASSED"},
		Env:              map[string]string{"ENV_NAME": "production", "DEPLOY": "true"},
	}

	ctxOneFailed := ConditionContext{
		UpstreamStatuses: map[string]string{"jobA": "PASSED", "jobB": "FAILED"},
		Env:              map[string]string{"ENV_NAME": "staging", "DEPLOY": "false"},
	}

	tests := []struct {
		expr     string
		ctx      ConditionContext
		expected bool
	}{
		// Default / success()
		{"", ctxAllPassed, true},
		{"success()", ctxAllPassed, true},
		{"success()", ctxOneFailed, false},

		// always()
		{"always()", ctxAllPassed, true},
		{"always()", ctxOneFailed, true},

		// failure()
		{"failure()", ctxAllPassed, false},
		{"failure()", ctxOneFailed, true},

		// env comparison
		{"env.ENV_NAME == 'production'", ctxAllPassed, true},
		{"env.ENV_NAME == 'production'", ctxOneFailed, false},
		{"${{ env.DEPLOY == 'true' }}", ctxAllPassed, true},
		{"env.ENV_NAME != 'production'", ctxOneFailed, true},

		// booleans
		{"true", ctxAllPassed, true},
		{"false", ctxAllPassed, false},
	}

	for _, tt := range tests {
		result, err := EvaluateCondition(tt.expr, tt.ctx)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.expr, err)
		}
		if result != tt.expected {
			t.Errorf("expr %q: expected %v, got %v", tt.expr, tt.expected, result)
		}
	}
}

func TestEvaluateConditionInvalid(t *testing.T) {
	_, err := EvaluateCondition("invalid_func()", ConditionContext{})
	if err == nil {
		t.Error("expected error for invalid expression, got nil")
	}
}
