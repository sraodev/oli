package app

import (
	"reflect"
	"testing"

	"github.com/sraodev/oli/internal/cleanup"
)

func fixtureRules() []cleanup.RuleInfo {
	return []cleanup.RuleInfo{
		{ID: "cache", Auto: true, Action: cleanup.ActionClean},
		{ID: "dev", Action: cleanup.ActionClean},
		{ID: "backup", Action: cleanup.ActionScanOnly},
	}
}

func TestRuleIDs(t *testing.T) {
	tests := []struct {
		name       string
		profile    string
		forCleanup bool
		want       []string
	}{
		{name: "safe", profile: ProfileSafe, want: []string{"cache"}},
		{name: "balanced", profile: ProfileBalanced, want: []string{"cache", "dev"}},
		{name: "review scan", profile: ProfileReview, want: []string{"cache", "dev", "backup"}},
		{name: "all cleanup", profile: ProfileAll, forCleanup: true, want: []string{"cache", "dev"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := RuleIDs(fixtureRules(), test.profile, test.forCleanup)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("RuleIDs() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestValidateRuleIDsRejectsReviewOnly(t *testing.T) {
	if _, err := ValidateRuleIDs(fixtureRules(), []string{"backup"}); err == nil {
		t.Fatal("ValidateRuleIDs accepted review-only rule")
	}
}

func TestValidateRuleIDsPreservesCatalogueOrder(t *testing.T) {
	got, err := ValidateRuleIDs(fixtureRules(), []string{"dev", "cache", "dev"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cache", "dev"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ValidateRuleIDs() = %q, want %q", got, want)
	}
}

func TestValidateScanRuleIDsAllowsReviewOnlyAndPreservesOrder(t *testing.T) {
	got, err := ValidateScanRuleIDs(fixtureRules(), []string{"backup", "cache"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cache", "backup"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ValidateScanRuleIDs() = %q, want %q", got, want)
	}
}
