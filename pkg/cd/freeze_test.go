package cd

import (
	"testing"
	"time"
)

func TestFreezeDateRange(t *testing.T) {
	fm := NewFreezeManager()

	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 7, 23, 59, 59, 0, time.UTC)

	fm.AddRule(&FreezeRule{
		ID:           "golden-week-2026",
		Name:         "National Day Holiday Freeze",
		Type:         FreezeDateRange,
		Environments: []string{"production", "prod"},
		StartTime:    start,
		EndTime:      end,
		AllowBypass:  true,
		BypassTokens: []string{"HOTFIX-SECRET-PASS"},
	})

	// 1. Before holiday
	before := time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC)
	allowed, _ := fm.CheckDeployment("production", before, "")
	if !allowed {
		t.Errorf("expected deployment to be allowed before freeze start")
	}

	// 2. During holiday without bypass
	during := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	allowed, reason := fm.CheckDeployment("production", during, "")
	if allowed {
		t.Errorf("expected deployment to be blocked during freeze")
	}
	if reason == "" {
		t.Errorf("expected non-empty reason for blockage")
	}

	// 3. During holiday with wrong bypass
	allowed, _ = fm.CheckDeployment("production", during, "WRONG-TOKEN")
	if allowed {
		t.Errorf("expected invalid token to be blocked")
	}

	// 4. During holiday with correct bypass
	allowed, _ = fm.CheckDeployment("production", during, "HOTFIX-SECRET-PASS")
	if !allowed {
		t.Errorf("expected valid bypass token to permit deployment")
	}

	// 5. Staging environment should not be affected
	allowed, _ = fm.CheckDeployment("staging", during, "")
	if !allowed {
		t.Errorf("staging environment should not be blocked by production freeze")
	}

	// 6. After holiday
	after := time.Date(2026, 10, 8, 1, 0, 0, 0, time.UTC)
	allowed, _ = fm.CheckDeployment("production", after, "")
	if !allowed {
		t.Errorf("expected deployment to be allowed after freeze expiry")
	}
}

func TestFreezeWeekly(t *testing.T) {
	fm := NewFreezeManager()

	// Every Friday 18:00 to 23:59
	fm.AddRule(&FreezeRule{
		ID:           "friday-deploy-freeze",
		Name:         "Friday Evening Release Freeze",
		Type:         FreezeWeekly,
		Environments: []string{"*"},
		Weekdays:     []time.Weekday{time.Friday},
		StartHour:    18,
		StartMinute:  0,
		EndHour:      23,
		EndMinute:    59,
	})

	// Friday 15:00 UTC (before 18:00) -> allowed
	// (2026-10-02 is a Friday)
	fridayBefore := time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)
	allowed, _ := fm.CheckDeployment("prod", fridayBefore, "")
	if !allowed {
		t.Errorf("expected Friday 15:00 to be allowed")
	}

	// Friday 19:30 UTC -> frozen
	fridayDuring := time.Date(2026, 10, 2, 19, 30, 0, 0, time.UTC)
	allowed, _ = fm.CheckDeployment("prod", fridayDuring, "")
	if allowed {
		t.Errorf("expected Friday 19:30 to be frozen")
	}

	// Thursday 19:30 UTC -> allowed
	// (2026-10-01 is Thursday)
	thursday := time.Date(2026, 10, 1, 19, 30, 0, 0, time.UTC)
	allowed, _ = fm.CheckDeployment("prod", thursday, "")
	if !allowed {
		t.Errorf("expected Thursday to be allowed")
	}
}
