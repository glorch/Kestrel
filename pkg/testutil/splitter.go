package testutil

import (
	"fmt"
	"sort"
	"time"
)

// SplitResult represents a partitioned slice of tests assigned to a runner.
type SplitResult struct {
	Index         int           `json:"index"`
	TotalSplits   int           `json:"total_splits"`
	Tests         []string      `json:"tests"`
	EstimatedTime time.Duration `json:"estimated_time"`
}

// Splitter calculates balanced partitions of test suites across parallel runners.
type Splitter struct{}

// NewSplitter creates a new test splitter instance.
func NewSplitter() *Splitter {
	return &Splitter{}
}

// SplitTests distributes test items across totalSplits partitions.
// If historical execution timings are provided, it uses a greedy longest-processing-time-first algorithm
// to minimize the makespan (difference between fastest and slowest slice).
func (s *Splitter) SplitTests(tests []string, totalSplits int, timings map[string]time.Duration) []SplitResult {
	if totalSplits <= 0 {
		totalSplits = 1
	}

	results := make([]SplitResult, totalSplits)
	for i := 0; i < totalSplits; i++ {
		results[i] = SplitResult{
			Index:       i,
			TotalSplits: totalSplits,
			Tests:       make([]string, 0),
		}
	}

	if len(tests) == 0 {
		return results
	}

	// 1. If timings provided, sort tests by descending duration (Longest Processing Time first)
	type testWithDuration struct {
		name string
		dur  time.Duration
	}

	items := make([]testWithDuration, len(tests))
	hasTimings := len(timings) > 0

	for i, t := range tests {
		d := 10 * time.Millisecond // default nominal duration
		if hasTimings {
			if historical, ok := timings[t]; ok && historical > 0 {
				d = historical
			}
		}
		items[i] = testWithDuration{name: t, dur: d}
	}

	if hasTimings {
		sort.Slice(items, func(i, j int) bool {
			return items[i].dur > items[j].dur
		})
	} else {
		// Deterministic alphabetical sort to ensure reproducible slices
		sort.Slice(items, func(i, j int) bool {
			return items[i].name < items[j].name
		})
	}

	// 2. Greedy assignment: place next test into the slice with lowest cumulative time
	for _, item := range items {
		minSliceIdx := 0
		minSliceTime := results[0].EstimatedTime

		for i := 1; i < totalSplits; i++ {
			if results[i].EstimatedTime < minSliceTime {
				minSliceTime = results[i].EstimatedTime
				minSliceIdx = i
			}
		}

		results[minSliceIdx].Tests = append(results[minSliceIdx].Tests, item.name)
		results[minSliceIdx].EstimatedTime += item.dur
	}

	return results
}

// GetSliceForIndex returns only the tests assigned to the requested split index.
func (s *Splitter) GetSliceForIndex(tests []string, totalSplits int, splitIndex int, timings map[string]time.Duration) ([]string, error) {
	if splitIndex < 0 || splitIndex >= totalSplits {
		return nil, fmt.Errorf("split index %d out of bounds (total: %d)", splitIndex, totalSplits)
	}

	all := s.SplitTests(tests, totalSplits, timings)
	return all[splitIndex].Tests, nil
}
