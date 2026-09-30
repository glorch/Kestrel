package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/glorch/kestrel/pkg/store"
)

func TestDORAEmptyRuns(t *testing.T) {
	st := store.NewMemoryStore()
	analyzer := NewDORAAnalyzer(st)

	report, err := analyzer.ComputeMetrics(context.Background(), 30)
	if err != nil {
		t.Fatalf("compute error: %v", err)
	}

	if report.TotalRuns != 0 {
		t.Errorf("expected 0 total runs, got: %d", report.TotalRuns)
	}
	if report.OverallTier != TierLow {
		t.Errorf("expected Low overall tier for empty history, got: %s", report.OverallTier)
	}
}

func TestDORAElitePerformance(t *testing.T) {
	st := store.NewMemoryStore()
	ctx := context.Background()

	baseTime := time.Now().Add(-10 * 24 * time.Hour)

	// Create 20 successful runs and 1 failed run over the last 10 days
	for i := 0; i < 20; i++ {
		startTime := baseTime.Add(time.Duration(i*12) * time.Hour)
		finishTime := startTime.Add(15 * time.Minute)
		run := &store.PipelineRun{
			ID:           formatTestID("run-pass", i),
			PipelineName: "main-service",
			Status:       "PASSED",
			StartedAt:    startTime,
			FinishedAt:   &finishTime,
			DurationMs:   15 * 60 * 1000, // 15 mins
		}
		_ = st.CreateRun(ctx, run)
	}

	// 1 failure followed quickly by recovery
	failStart := baseTime.Add(2 * 24 * time.Hour)
	failFinish := failStart.Add(5 * time.Minute)
	failRun := &store.PipelineRun{
		ID:           "run-fail-1",
		PipelineName: "main-service",
		Status:       "FAILED",
		StartedAt:    failStart,
		FinishedAt:   &failFinish,
		DurationMs:   5 * 60 * 1000,
	}
	_ = st.CreateRun(ctx, failRun)

	analyzer := NewDORAAnalyzer(st)
	report, err := analyzer.ComputeMetrics(ctx, 30)
	if err != nil {
		t.Fatalf("compute failed: %v", err)
	}

	if report.TotalRuns != 21 {
		t.Errorf("expected 21 runs, got: %d", report.TotalRuns)
	}
	if report.SuccessfulRuns != 20 || report.FailedRuns != 1 {
		t.Errorf("expected 20 passes and 1 fail, got pass=%d, fail=%d", report.SuccessfulRuns, report.FailedRuns)
	}

	// Change failure rate should be 1/21 ~ 4.76% (Elite <= 5%)
	if report.ChangeFailureTier != TierElite {
		t.Errorf("expected ChangeFailureTier Elite, got: %s (rate: %.4f)", report.ChangeFailureTier, report.ChangeFailureRate)
	}

	// Lead time is 15 mins (Elite < 1 hour)
	if report.LeadTimeTier != TierElite {
		t.Errorf("expected LeadTimeTier Elite, got: %s (lead time: %s)", report.LeadTimeTier, report.LeadTimeMedian)
	}

	if report.OverallTier != TierElite && report.OverallTier != TierHigh {
		t.Errorf("expected OverallTier Elite/High, got: %s", report.OverallTier)
	}
}

func formatTestID(prefix string, idx int) string {
	return prefix + "-" + time.Now().Format("150405") + "-" + string(rune('a'+idx))
}
