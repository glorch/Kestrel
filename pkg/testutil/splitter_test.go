package testutil

import (
	"testing"
	"time"
)

func TestSplitterAlphabetical(t *testing.T) {
	splitter := NewSplitter()
	tests := []string{
		"test_auth.py",
		"test_billing.py",
		"test_cart.py",
		"test_checkout.py",
		"test_order.py",
		"test_user.py",
	}

	results := splitter.SplitTests(tests, 2, nil)
	if len(results) != 2 {
		t.Fatalf("expected 2 splits, got %d", len(results))
	}

	// 6 tests across 2 splits should give 3 tests each
	if len(results[0].Tests) != 3 || len(results[1].Tests) != 3 {
		t.Errorf("expected 3 tests per split, got: %d and %d", len(results[0].Tests), len(results[1].Tests))
	}

	slice0, err := splitter.GetSliceForIndex(tests, 2, 0, nil)
	if err != nil {
		t.Fatalf("failed to get slice 0: %v", err)
	}
	if len(slice0) != 3 {
		t.Errorf("expected slice 0 to have 3 items, got %d", len(slice0))
	}
}

func TestSplitterTimingGreedyBalance(t *testing.T) {
	splitter := NewSplitter()

	tests := []string{
		"test_heavy_e2e",    // 50s
		"test_medium_api_1", // 20s
		"test_medium_api_2", // 20s
		"test_light_unit_1", // 5s
		"test_light_unit_2", // 5s
	}

	timings := map[string]time.Duration{
		"test_heavy_e2e":    50 * time.Second,
		"test_medium_api_1": 20 * time.Second,
		"test_medium_api_2": 20 * time.Second,
		"test_light_unit_1": 5 * time.Second,
		"test_light_unit_2": 5 * time.Second,
	}

	// Total time = 100s. Split into 2 shards. Perfect balance = 50s each!
	// Shard 1 should get test_heavy_e2e (50s)
	// Shard 2 should get 20 + 20 + 5 + 5 = 50s!
	results := splitter.SplitTests(tests, 2, timings)
	if len(results) != 2 {
		t.Fatalf("expected 2 splits, got: %d", len(results))
	}

	s0 := results[0].EstimatedTime
	s1 := results[1].EstimatedTime

	if s0 != 50*time.Second || s1 != 50*time.Second {
		t.Errorf("expected both splits to take 50s, got s0=%s, s1=%s", s0, s1)
	}
}
