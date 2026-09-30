package cd

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CanaryStatus represents the overall state of a canary rollout.
type CanaryStatus string

const (
	CanaryPending    CanaryStatus = "PENDING"
	CanaryProgressing CanaryStatus = "PROGRESSING"
	CanaryPromoted    CanaryStatus = "PROMOTED"
	CanaryRollback    CanaryStatus = "ROLLBACK"
)

// MetricOperator defines how a metric value is evaluated against its threshold.
type MetricOperator string

const (
	OpLessThanOrEqual    MetricOperator = "<="
	OpGreaterThanOrEqual MetricOperator = ">="
	OpLessThan           MetricOperator = "<"
	OpGreaterThan        MetricOperator = ">"
)

// MetricRule defines a healthy threshold and tolerance for a key business or system metric.
type MetricRule struct {
	Name         string         `json:"name" yaml:"name"`
	Operator     MetricOperator `json:"operator" yaml:"operator"`
	Threshold    float64        `json:"threshold" yaml:"threshold"`
	FailureLimit int            `json:"failure_limit" yaml:"failure_limit"` // Number of consecutive failed checks before triggering auto-rollback
}

// CanaryStep defines an incremental traffic weight and observation pause.
type CanaryStep struct {
	SetWeight     int           `json:"set_weight" yaml:"set_weight"` // 1 to 100 percentage
	PauseDuration time.Duration `json:"pause_duration" yaml:"pause_duration"`
}

// CanarySpec defines the full specification for progressive traffic rollout and automated metric analysis.
type CanarySpec struct {
	Name          string        `json:"name" yaml:"name"`
	Steps         []CanaryStep  `json:"steps" yaml:"steps"`
	Metrics       []MetricRule  `json:"metrics" yaml:"metrics"`
	CheckInterval time.Duration `json:"check_interval" yaml:"check_interval"`
}

// MetricProvider queries metrics from external monitoring systems (e.g., Prometheus, Datadog, CloudWatch).
type MetricProvider interface {
	QueryMetric(ctx context.Context, metricName string) (float64, error)
}

// CanaryStepResult documents the outcome and metrics collected during a rollout step.
type CanaryStepResult struct {
	StepIndex    int                `json:"step_index"`
	Weight       int                `json:"weight"`
	Passed       bool               `json:"passed"`
	Violations   []string           `json:"violations,omitempty"`
	SampleValues map[string]float64 `json:"sample_values"`
	ObservedAt   time.Time          `json:"observed_at"`
}

// RolloutReport provides a comprehensive post-rollout audit record.
type RolloutReport struct {
	CanaryName    string             `json:"canary_name"`
	Status        CanaryStatus       `json:"status"`
	FinalWeight   int                `json:"final_weight"`
	StepResults   []CanaryStepResult `json:"step_results"`
	TotalDuration time.Duration      `json:"total_duration"`
	Reason        string             `json:"reason,omitempty"`
}

// CanaryAnalyzer executes canary steps, samples live telemetry, and determines whether to promote or rollback.
type CanaryAnalyzer struct {
	mu sync.RWMutex
}

// NewCanaryAnalyzer initializes a new canary analyzer.
func NewCanaryAnalyzer() *CanaryAnalyzer {
	return &CanaryAnalyzer{}
}

// EvaluateMetrics compares queried values against rule thresholds.
func (ca *CanaryAnalyzer) EvaluateMetrics(rules []MetricRule, values map[string]float64) (bool, []string) {
	var violations []string

	for _, rule := range rules {
		val, exists := values[rule.Name]
		if !exists {
			violations = append(violations, fmt.Sprintf("metric '%s' was not reported", rule.Name))
			continue
		}

		passed := false
		switch rule.Operator {
		case OpLessThanOrEqual, "":
			passed = val <= rule.Threshold
		case OpLessThan:
			passed = val < rule.Threshold
		case OpGreaterThanOrEqual:
			passed = val >= rule.Threshold
		case OpGreaterThan:
			passed = val > rule.Threshold
		default:
			violations = append(violations, fmt.Sprintf("unknown metric operator '%s' for '%s'", rule.Operator, rule.Name))
			continue
		}

		if !passed {
			violations = append(violations, fmt.Sprintf("metric '%s' violated constraint: got %.4f %s %.4f",
				rule.Name, val, rule.Operator, rule.Threshold))
		}
	}

	return len(violations) == 0, violations
}

// TrafficShiftHook is a callback invoked when traffic percentage needs to be shifted.
type TrafficShiftHook func(ctx context.Context, targetWeight int) error

// RunRollout coordinates the progressive traffic migration and monitors health metrics at each stage.
func (ca *CanaryAnalyzer) RunRollout(
	ctx context.Context,
	spec *CanarySpec,
	provider MetricProvider,
	shiftTraffic TrafficShiftHook,
) (*RolloutReport, error) {
	startTime := time.Now()

	report := &RolloutReport{
		CanaryName:  spec.Name,
		Status:      CanaryProgressing,
		FinalWeight: 0,
		StepResults: make([]CanaryStepResult, 0, len(spec.Steps)),
	}

	if len(spec.Steps) == 0 {
		report.Status = CanaryPromoted
		report.FinalWeight = 100
		report.TotalDuration = time.Since(startTime)
		return report, nil
	}

	// Map to track consecutive failures per metric
	consecutiveFailures := make(map[string]int)

	for idx, step := range spec.Steps {
		// 1. Shift traffic weight
		if shiftTraffic != nil {
			if err := shiftTraffic(ctx, step.SetWeight); err != nil {
				report.Status = CanaryRollback
				report.Reason = fmt.Sprintf("failed to shift traffic to %d%% at step %d: %v", step.SetWeight, idx+1, err)
				report.TotalDuration = time.Since(startTime)
				_ = shiftTraffic(context.Background(), 0) // Rollback to 0
				return report, fmt.Errorf("%s", report.Reason)
			}
		}
		report.FinalWeight = step.SetWeight

		// 2. Pause and sample metrics
		stepPassed := true
		var stepViolations []string
		sampleValues := make(map[string]float64)

		pause := step.PauseDuration
		if pause <= 0 {
			pause = 10 * time.Millisecond
		}

		select {
		case <-ctx.Done():
			report.Status = CanaryRollback
			report.Reason = fmt.Sprintf("canary execution cancelled: %v", ctx.Err())
			report.TotalDuration = time.Since(startTime)
			if shiftTraffic != nil {
				_ = shiftTraffic(context.Background(), 0)
			}
			return report, ctx.Err()
		case <-time.After(pause):
		}

		// 3. Query metrics from provider
		if provider != nil && len(spec.Metrics) > 0 {
			for _, rule := range spec.Metrics {
				val, err := provider.QueryMetric(ctx, rule.Name)
				if err != nil {
					stepPassed = false
					violation := fmt.Sprintf("failed to query metric '%s': %v", rule.Name, err)
					stepViolations = append(stepViolations, violation)
					consecutiveFailures[rule.Name]++
					continue
				}
				sampleValues[rule.Name] = val

				// Single rule evaluation
				passed, viol := ca.EvaluateMetrics([]MetricRule{rule}, map[string]float64{rule.Name: val})
				if !passed {
					consecutiveFailures[rule.Name]++
					stepViolations = append(stepViolations, viol...)
					limit := rule.FailureLimit
					if limit <= 0 {
						limit = 1
					}
					if consecutiveFailures[rule.Name] >= limit {
						stepPassed = false
					}
				} else {
					consecutiveFailures[rule.Name] = 0 // Reset on healthy check
				}
			}
		}

		stepResult := CanaryStepResult{
			StepIndex:    idx + 1,
			Weight:       step.SetWeight,
			Passed:       stepPassed,
			Violations:   stepViolations,
			SampleValues: sampleValues,
			ObservedAt:   time.Now(),
		}
		report.StepResults = append(report.StepResults, stepResult)

		// 4. Auto-rollback if health checks failed
		if !stepPassed {
			report.Status = CanaryRollback
			report.Reason = fmt.Sprintf("metric failure limit reached at %d%% traffic: %v", step.SetWeight, stepViolations)
			report.TotalDuration = time.Since(startTime)

			if shiftTraffic != nil {
				_ = shiftTraffic(context.Background(), 0) // Immediate auto-rollback
			}
			report.FinalWeight = 0
			return report, nil
		}
	}

	report.Status = CanaryPromoted
	report.TotalDuration = time.Since(startTime)
	return report, nil
}
