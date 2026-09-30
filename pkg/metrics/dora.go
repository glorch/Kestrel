package metrics

import (
	"context"
	"sort"
	"time"

	"github.com/glorch/kestrel/pkg/store"
)

// RatingTier defines the DORA benchmark classification levels.
type RatingTier string

const (
	TierElite  RatingTier = "Elite"
	TierHigh   RatingTier = "High"
	TierMedium RatingTier = "Medium"
	TierLow    RatingTier = "Low"
)

// DORAReport represents aggregated DevOps delivery and operational performance metrics.
type DORAReport struct {
	TotalRuns           int           `json:"total_runs"`
	SuccessfulRuns      int           `json:"successful_runs"`
	FailedRuns          int           `json:"failed_runs"`
	DeploymentFrequency float64       `json:"deployment_frequency_per_day"` // Deployments per day
	DeploymentFreqTier  RatingTier    `json:"deployment_frequency_tier"`
	LeadTimeMedian      time.Duration `json:"lead_time_median"`
	LeadTimeTier        RatingTier    `json:"lead_time_tier"`
	ChangeFailureRate   float64       `json:"change_failure_rate"` // e.g. 0.05 = 5%
	ChangeFailureTier   RatingTier    `json:"change_failure_tier"`
	MeanTimeToRestore   time.Duration `json:"mean_time_to_restore"`
	MTTRTier            RatingTier    `json:"mttr_tier"`
	OverallTier         RatingTier    `json:"overall_tier"`
	WindowDays          int           `json:"window_days"`
}

// DORAAnalyzer queries historical run data and computes key engineering metrics.
type DORAAnalyzer struct {
	store store.Store
}

// NewDORAAnalyzer creates a new DORA metric calculator.
func NewDORAAnalyzer(st store.Store) *DORAAnalyzer {
	return &DORAAnalyzer{
		store: st,
	}
}

// ComputeMetrics aggregates metrics for runs over the specified time window.
func (da *DORAAnalyzer) ComputeMetrics(ctx context.Context, windowDays int) (*DORAReport, error) {
	if windowDays <= 0 {
		windowDays = 30
	}

	since := time.Now().Add(-time.Duration(windowDays) * 24 * time.Hour)
	runs, err := da.store.ListRuns(ctx, store.RunFilter{
		Limit: 10000,
	})
	if err != nil {
		return nil, err
	}

	report := &DORAReport{
		WindowDays: windowDays,
	}

	var filteredRuns []*store.PipelineRun
	for _, r := range runs {
		if r.StartedAt.After(since) || r.StartedAt.Equal(since) {
			filteredRuns = append(filteredRuns, r)
		}
	}

	report.TotalRuns = len(filteredRuns)
	if report.TotalRuns == 0 {
		report.DeploymentFreqTier = TierLow
		report.LeadTimeTier = TierLow
		report.ChangeFailureTier = TierElite
		report.MTTRTier = TierElite
		report.OverallTier = TierLow
		return report, nil
	}

	var leadTimes []time.Duration
	var restoreTimes []time.Duration
	var lastFailedAt *time.Time

	// Sort chronological
	sort.Slice(filteredRuns, func(i, j int) bool {
		return filteredRuns[i].StartedAt.Before(filteredRuns[j].StartedAt)
	})

	for _, r := range filteredRuns {
		if r.Status == "PASSED" {
			report.SuccessfulRuns++
			leadTimes = append(leadTimes, time.Duration(r.DurationMs)*time.Millisecond)

			if lastFailedAt != nil {
				restoreTimes = append(restoreTimes, r.StartedAt.Sub(*lastFailedAt))
				lastFailedAt = nil
			}
		} else if r.Status == "FAILED" {
			report.FailedRuns++
			if lastFailedAt == nil {
				t := r.StartedAt
				lastFailedAt = &t
			}
		}
	}

	// 1. Deployment Frequency
	report.DeploymentFrequency = float64(report.SuccessfulRuns) / float64(windowDays)
	if report.DeploymentFrequency >= 1.0 {
		report.DeploymentFreqTier = TierElite // >= 1 deploy/day
	} else if report.DeploymentFrequency >= 0.14 {
		report.DeploymentFreqTier = TierHigh // >= 1 deploy/week
	} else if report.DeploymentFrequency >= 0.033 {
		report.DeploymentFreqTier = TierMedium // >= 1 deploy/month
	} else {
		report.DeploymentFreqTier = TierLow
	}

	// 2. Lead Time for Changes (Median)
	if len(leadTimes) > 0 {
		sort.Slice(leadTimes, func(i, j int) bool {
			return leadTimes[i] < leadTimes[j]
		})
		report.LeadTimeMedian = leadTimes[len(leadTimes)/2]
		if report.LeadTimeMedian < 1*time.Hour {
			report.LeadTimeTier = TierElite
		} else if report.LeadTimeMedian < 24*time.Hour {
			report.LeadTimeTier = TierHigh
		} else if report.LeadTimeMedian < 7*24*time.Hour {
			report.LeadTimeTier = TierMedium
		} else {
			report.LeadTimeTier = TierLow
		}
	} else {
		report.LeadTimeTier = TierLow
	}

	// 3. Change Failure Rate
	totalDecided := report.SuccessfulRuns + report.FailedRuns
	if totalDecided > 0 {
		report.ChangeFailureRate = float64(report.FailedRuns) / float64(totalDecided)
		if report.ChangeFailureRate <= 0.05 {
			report.ChangeFailureTier = TierElite
		} else if report.ChangeFailureRate <= 0.15 {
			report.ChangeFailureTier = TierHigh
		} else if report.ChangeFailureRate <= 0.30 {
			report.ChangeFailureTier = TierMedium
		} else {
			report.ChangeFailureTier = TierLow
		}
	} else {
		report.ChangeFailureTier = TierElite
	}

	// 4. Mean Time to Restore (MTTR)
	if len(restoreTimes) > 0 {
		var sum time.Duration
		for _, rt := range restoreTimes {
			sum += rt
		}
		report.MeanTimeToRestore = sum / time.Duration(len(restoreTimes))
		if report.MeanTimeToRestore < 1*time.Hour {
			report.MTTRTier = TierElite
		} else if report.MeanTimeToRestore < 24*time.Hour {
			report.MTTRTier = TierHigh
		} else if report.MeanTimeToRestore < 7*24*time.Hour {
			report.MTTRTier = TierMedium
		} else {
			report.MTTRTier = TierLow
		}
	} else {
		report.MTTRTier = TierElite // No failures requiring restoration
	}

	// Calculate overall composite tier
	report.OverallTier = computeCompositeTier([]RatingTier{
		report.DeploymentFreqTier,
		report.LeadTimeTier,
		report.ChangeFailureTier,
		report.MTTRTier,
	})

	return report, nil
}

func computeCompositeTier(tiers []RatingTier) RatingTier {
	var score int
	for _, t := range tiers {
		switch t {
		case TierElite:
			score += 4
		case TierHigh:
			score += 3
		case TierMedium:
			score += 2
		case TierLow:
			score += 1
		}
	}
	avg := float64(score) / float64(len(tiers))
	if avg >= 3.5 {
		return TierElite
	} else if avg >= 2.5 {
		return TierHigh
	} else if avg >= 1.5 {
		return TierMedium
	}
	return TierLow
}
