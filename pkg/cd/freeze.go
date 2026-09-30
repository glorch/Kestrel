package cd

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// FreezeType defines whether the freeze window is recurring or a one-off date range.
type FreezeType string

const (
	FreezeWeekly    FreezeType = "weekly"
	FreezeDateRange FreezeType = "daterange"
)

// FreezeRule defines conditions under which deployments to specific environments are prohibited.
type FreezeRule struct {
	ID           string         `json:"id" yaml:"id"`
	Name         string         `json:"name" yaml:"name"`
	Type         FreezeType     `json:"type" yaml:"type"`
	Environments []string       `json:"environments" yaml:"environments"` // Targets, e.g. ["production", "prod"]
	
	// For FreezeDateRange
	StartTime    time.Time      `json:"start_time,omitempty" yaml:"start_time,omitempty"`
	EndTime      time.Time      `json:"end_time,omitempty" yaml:"end_time,omitempty"`

	// For FreezeWeekly (e.g. every Friday 16:00 to Monday 09:00)
	Weekdays     []time.Weekday `json:"weekdays,omitempty" yaml:"weekdays,omitempty"`
	StartHour    int            `json:"start_hour,omitempty" yaml:"start_hour,omitempty"` // 0-23
	StartMinute  int            `json:"start_minute,omitempty" yaml:"start_minute,omitempty"`
	EndHour      int            `json:"end_hour,omitempty" yaml:"end_hour,omitempty"`
	EndMinute    int            `json:"end_minute,omitempty" yaml:"end_minute,omitempty"`

	// Emergency bypass controls
	AllowBypass  bool           `json:"allow_bypass" yaml:"allow_bypass"`
	BypassTokens []string       `json:"bypass_tokens,omitempty" yaml:"bypass_tokens,omitempty"`
}

// FreezeManager coordinates change freeze enforcement across all environments.
type FreezeManager struct {
	mu    sync.RWMutex
	rules map[string]*FreezeRule
}

// NewFreezeManager creates an empty FreezeManager.
func NewFreezeManager() *FreezeManager {
	return &FreezeManager{
		rules: make(map[string]*FreezeRule),
	}
}

// AddRule registers a new change freeze rule.
func (fm *FreezeManager) AddRule(rule *FreezeRule) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.rules[rule.ID] = rule
}

// RemoveRule deletes a rule by ID.
func (fm *FreezeManager) RemoveRule(id string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	delete(fm.rules, id)
}

// ListRules returns all active freeze rules.
func (fm *FreezeManager) ListRules() []*FreezeRule {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	list := make([]*FreezeRule, 0, len(fm.rules))
	for _, r := range fm.rules {
		list = append(list, r)
	}
	return list
}

// IsFrozen checks whether the target environment is currently frozen at the given timestamp.
func (fm *FreezeManager) IsFrozen(env string, at time.Time) (bool, *FreezeRule) {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	for _, rule := range fm.rules {
		if !ruleMatchesEnv(rule, env) {
			continue
		}

		if ruleActiveAt(rule, at) {
			return true, rule
		}
	}

	return false, nil
}

// CheckDeployment evaluates whether deployment is permitted, considering emergency bypass.
func (fm *FreezeManager) CheckDeployment(env string, at time.Time, bypassToken string) (bool, string) {
	frozen, rule := fm.IsFrozen(env, at)
	if !frozen {
		return true, ""
	}

	// If frozen, check if valid bypass token was supplied
	if rule.AllowBypass && bypassToken != "" {
		for _, token := range rule.BypassTokens {
			if token == bypassToken {
				return true, fmt.Sprintf("deployment allowed under bypass authorization for rule '%s'", rule.Name)
			}
		}
	}

	return false, fmt.Sprintf("environment '%s' is under active change freeze '%s' (rule ID: %s)", env, rule.Name, rule.ID)
}

func ruleMatchesEnv(rule *FreezeRule, env string) bool {
	if len(rule.Environments) == 0 {
		return strings.EqualFold(env, "production") || strings.EqualFold(env, "prod")
	}
	for _, target := range rule.Environments {
		if strings.EqualFold(target, env) || target == "*" {
			return true
		}
	}
	return false
}

func ruleActiveAt(rule *FreezeRule, at time.Time) bool {
	switch rule.Type {
	case FreezeDateRange:
		return (at.Equal(rule.StartTime) || at.After(rule.StartTime)) &&
			(at.Equal(rule.EndTime) || at.Before(rule.EndTime))

	case FreezeWeekly:
		weekday := at.Weekday()
		weekdayMatched := false
		for _, w := range rule.Weekdays {
			if w == weekday {
				weekdayMatched = true
				break
			}
		}
		if !weekdayMatched {
			return false
		}

		minuteOfDay := at.Hour()*60 + at.Minute()
		startMinuteOfDay := rule.StartHour*60 + rule.StartMinute
		endMinuteOfDay := rule.EndHour*60 + rule.EndMinute

		if startMinuteOfDay <= endMinuteOfDay {
			return minuteOfDay >= startMinuteOfDay && minuteOfDay <= endMinuteOfDay
		}
		// Crosses midnight (e.g. 22:00 to 04:00)
		return minuteOfDay >= startMinuteOfDay || minuteOfDay <= endMinuteOfDay

	default:
		return false
	}
}
