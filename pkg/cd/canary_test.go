package cd

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

type mockMetricProvider struct {
	mu      sync.Mutex
	metrics map[string]float64
}

func (m *mockMetricProvider) QueryMetric(ctx context.Context, name string) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	val, ok := m.metrics[name]
	if !ok {
		return 0, fmt.Errorf("metric not found: %s", name)
	}
	return val, nil
}

func (m *mockMetricProvider) setMetric(name string, val float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics[name] = val
}

func TestCanaryEvaluateMetrics(t *testing.T) {
	analyzer := NewCanaryAnalyzer()

	rules := []MetricRule{
		{
			Name:      "error-rate",
			Operator:  OpLessThanOrEqual,
			Threshold: 0.02,
		},
		{
			Name:      "p99-latency",
			Operator:  OpLessThan,
			Threshold: 200.0,
		},
		{
			Name:      "availability",
			Operator:  OpGreaterThanOrEqual,
			Threshold: 99.9,
		},
	}

	// Healthy samples
	healthyValues := map[string]float64{
		"error-rate":   0.005,
		"p99-latency":  120.0,
		"availability": 99.95,
	}
	passed, violations := analyzer.EvaluateMetrics(rules, healthyValues)
	if !passed || len(violations) > 0 {
		t.Fatalf("expected healthy values to pass, got violations: %v", violations)
	}

	// Unhealthy samples
	unhealthyValues := map[string]float64{
		"error-rate":   0.05,  // Exceeds 0.02
		"p99-latency":  250.0, // Exceeds 200
		"availability": 99.0,  // Below 99.9
	}
	passed, violations = analyzer.EvaluateMetrics(rules, unhealthyValues)
	if passed {
		t.Fatal("expected unhealthy values to fail")
	}
	if len(violations) != 3 {
		t.Fatalf("expected 3 violations, got %d: %v", len(violations), violations)
	}
}

func TestCanarySuccessfulRollout(t *testing.T) {
	analyzer := NewCanaryAnalyzer()
	provider := &mockMetricProvider{
		metrics: map[string]float64{
			"error-rate":  0.005,
			"p99-latency": 80.0,
		},
	}

	spec := &CanarySpec{
		Name: "frontend-canary",
		Steps: []CanaryStep{
			{SetWeight: 10, PauseDuration: 10 * time.Millisecond},
			{SetWeight: 25, PauseDuration: 10 * time.Millisecond},
			{SetWeight: 50, PauseDuration: 10 * time.Millisecond},
			{SetWeight: 100, PauseDuration: 10 * time.Millisecond},
		},
		Metrics: []MetricRule{
			{Name: "error-rate", Operator: OpLessThanOrEqual, Threshold: 0.01, FailureLimit: 1},
			{Name: "p99-latency", Operator: OpLessThan, Threshold: 150.0, FailureLimit: 1},
		},
	}

	var weightHistory []int
	shiftTraffic := func(ctx context.Context, targetWeight int) error {
		weightHistory = append(weightHistory, targetWeight)
		return nil
	}

	ctx := context.Background()
	report, err := analyzer.RunRollout(ctx, spec, provider, shiftTraffic)
	if err != nil {
		t.Fatalf("expected rollout to succeed, got error: %v", err)
	}

	if report.Status != CanaryPromoted {
		t.Errorf("expected CanaryPromoted, got: %s", report.Status)
	}
	if report.FinalWeight != 100 {
		t.Errorf("expected final weight 100, got: %d", report.FinalWeight)
	}
	if len(report.StepResults) != 4 {
		t.Errorf("expected 4 step results, got: %d", len(report.StepResults))
	}
}

func TestCanaryAutoRollbackOnDegradation(t *testing.T) {
	analyzer := NewCanaryAnalyzer()
	provider := &mockMetricProvider{
		metrics: map[string]float64{
			"error-rate": 0.002,
		},
	}

	spec := &CanarySpec{
		Name: "payment-service-canary",
		Steps: []CanaryStep{
			{SetWeight: 10, PauseDuration: 10 * time.Millisecond},
			{SetWeight: 25, PauseDuration: 10 * time.Millisecond},
			{SetWeight: 50, PauseDuration: 10 * time.Millisecond},
		},
		Metrics: []MetricRule{
			{Name: "error-rate", Operator: OpLessThanOrEqual, Threshold: 0.01, FailureLimit: 1},
		},
	}

	var weightHistory []int
	shiftTraffic := func(ctx context.Context, targetWeight int) error {
		weightHistory = append(weightHistory, targetWeight)
		if targetWeight == 25 {
			// Simulate severe regression when 25% traffic is reached
			provider.setMetric("error-rate", 0.08)
		}
		return nil
	}

	ctx := context.Background()
	report, err := analyzer.RunRollout(ctx, spec, provider, shiftTraffic)
	if err != nil {
		t.Fatalf("expected graceful rollback report, got error: %v", err)
	}

	if report.Status != CanaryRollback {
		t.Fatalf("expected status CanaryRollback, got: %s", report.Status)
	}
	if report.FinalWeight != 0 {
		t.Errorf("expected final weight reset to 0, got: %d", report.FinalWeight)
	}
	// Last traffic weight shift should be 0 (rollback)
	if len(weightHistory) == 0 || weightHistory[len(weightHistory)-1] != 0 {
		t.Errorf("expected final traffic shift to 0, history was: %v", weightHistory)
	}
}
