package app

import (
	"fmt"

	"github.com/sraodev/oli/internal/cleanup"
)

const (
	ProfileSafe     = "safe"
	ProfileBalanced = "balanced"
	ProfileReview   = "review"
	ProfileAll      = "all"
)

// RuleIDs returns rule IDs in catalogue order. Cleanup selections never
// include scan-only rules, even for the broad profiles.
func RuleIDs(rules []cleanup.RuleInfo, profile string, forCleanup bool) ([]string, error) {
	var include func(cleanup.RuleInfo) bool
	switch profile {
	case ProfileSafe:
		include = func(rule cleanup.RuleInfo) bool {
			return rule.Auto && rule.Action == cleanup.ActionClean
		}
	case ProfileBalanced:
		include = func(rule cleanup.RuleInfo) bool {
			return rule.Action == cleanup.ActionClean
		}
	case ProfileReview:
		include = func(rule cleanup.RuleInfo) bool {
			return !forCleanup || rule.Action == cleanup.ActionClean
		}
	case ProfileAll:
		include = func(rule cleanup.RuleInfo) bool {
			return !forCleanup || rule.Action == cleanup.ActionClean
		}
	default:
		return nil, fmt.Errorf("unknown profile %q", profile)
	}

	ids := make([]string, 0, len(rules))
	for _, rule := range rules {
		if include(rule) {
			ids = append(ids, rule.ID)
		}
	}
	return ids, nil
}

// ValidateRuleIDs validates an explicit cleanup selection and returns it in
// catalogue order. Scan-only rules fail closed.
func ValidateRuleIDs(rules []cleanup.RuleInfo, requested []string) ([]string, error) {
	return validateRuleIDs(rules, requested, true)
}

// ValidateScanRuleIDs validates an explicit read-only selection and preserves
// catalogue order. Unlike cleanup selection, report-only rules are allowed.
func ValidateScanRuleIDs(rules []cleanup.RuleInfo, requested []string) ([]string, error) {
	return validateRuleIDs(rules, requested, false)
}

func validateRuleIDs(rules []cleanup.RuleInfo, requested []string, forCleanup bool) ([]string, error) {
	if len(requested) == 0 {
		return nil, cleanup.ErrNoRulesSelected
	}
	wanted := make(map[string]struct{}, len(requested))
	for _, id := range requested {
		wanted[id] = struct{}{}
	}

	selected := make([]string, 0, len(wanted))
	for _, rule := range rules {
		if _, ok := wanted[rule.ID]; !ok {
			continue
		}
		delete(wanted, rule.ID)
		if forCleanup && rule.Action != cleanup.ActionClean {
			return nil, fmt.Errorf("rule %q is review-only", rule.ID)
		}
		selected = append(selected, rule.ID)
	}
	for id := range wanted {
		return nil, fmt.Errorf("unknown rule %q", id)
	}
	return selected, nil
}
