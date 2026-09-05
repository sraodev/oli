package app

import (
	"testing"

	"github.com/sraodev/oli/internal/cleanup"
)

func TestRecommendationsAreDeterministicAndNeverAuthorizeDeletion(t *testing.T) {
	scan := &cleanup.Scan{Rules: []cleanup.RuleScan{
		{
			Rule:       cleanup.RuleInfo{ID: "safe", Name: "Safe", Risk: cleanup.RiskSafe, Action: cleanup.ActionClean, Auto: true},
			Candidates: []cleanup.Candidate{{Eligible: true}},
			Eligible:   cleanup.Metrics{Items: 1, AllocatedBytes: 10},
		},
		{
			Rule:       cleanup.RuleInfo{ID: "review", Name: "Review", Risk: cleanup.RiskCaution, Action: cleanup.ActionClean},
			Candidates: []cleanup.Candidate{{Eligible: true}},
			Eligible:   cleanup.Metrics{Items: 1, AllocatedBytes: 20},
		},
		{
			Rule:       cleanup.RuleInfo{ID: "report", Name: "Report", Risk: cleanup.RiskReview, Action: cleanup.ActionScanOnly},
			Candidates: []cleanup.Candidate{{}},
			Detected:   cleanup.Metrics{Items: 1, AllocatedBytes: 30},
		},
		{
			Rule: cleanup.RuleInfo{ID: "empty", Name: "Empty", Risk: cleanup.RiskSafe, Action: cleanup.ActionClean, Auto: true},
		},
	}}

	got := Recommendations(scan)
	want := []RecommendationDecision{DecisionAutoClean, DecisionReview, DecisionInspect, DecisionNoAction}
	if len(got) != len(want) {
		t.Fatalf("recommendations = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Decision != want[i] {
			t.Fatalf("recommendation %d decision = %q, want %q", i, got[i].Decision, want[i])
		}
		if got[i].Reason == "" {
			t.Fatalf("recommendation %d has no explanation", i)
		}
	}
}
