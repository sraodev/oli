package app

import "github.com/sraodev/mac-cleanup-studio/internal/cleanup"

// RecommendationDecision is an explainable, deterministic suggestion. It is
// intentionally not model-generated and never grants deletion authority.
type RecommendationDecision string

const (
	DecisionAutoClean RecommendationDecision = "auto_clean"
	DecisionReview    RecommendationDecision = "review"
	DecisionInspect   RecommendationDecision = "inspect"
	DecisionNoAction  RecommendationDecision = "no_action"
)

// Recommendation summarizes one scanned rule for humans and automation.
type Recommendation struct {
	RuleID             string                 `json:"rule_id"`
	Name               string                 `json:"name"`
	Risk               cleanup.Risk           `json:"risk"`
	Action             cleanup.Action         `json:"action"`
	Decision           RecommendationDecision `json:"decision"`
	Reason             string                 `json:"reason"`
	CandidateCount     int                    `json:"candidate_count"`
	EligibleCandidates int                    `json:"eligible_candidate_count"`
	Detected           cleanup.Metrics        `json:"detected"`
	Reclaimable        cleanup.Metrics        `json:"reclaimable"`
	MinimumAgeSeconds  int64                  `json:"minimum_age_seconds"`
}

// Recommendations derives suggestions only from the current in-process scan,
// compiled rule metadata, and explicit age/risk policy.
func Recommendations(scan *cleanup.Scan) []Recommendation {
	if scan == nil {
		return nil
	}
	out := make([]Recommendation, 0, len(scan.Rules))
	for _, scanned := range scan.Rules {
		decision, reason := recommendationFor(scanned)
		eligible := 0
		for _, candidate := range scanned.Candidates {
			if candidate.Eligible {
				eligible++
			}
		}
		out = append(out, Recommendation{
			RuleID: scanned.Rule.ID, Name: scanned.Rule.Name,
			Risk: scanned.Rule.Risk, Action: scanned.Rule.Action,
			Decision: decision, Reason: reason, CandidateCount: len(scanned.Candidates), EligibleCandidates: eligible,
			Detected: scanned.Detected, Reclaimable: scanned.Eligible,
			MinimumAgeSeconds: scanned.Rule.MinimumAgeSeconds,
		})
	}
	return out
}

func recommendationFor(scanned cleanup.RuleScan) (RecommendationDecision, string) {
	if scanned.Rule.Action == cleanup.ActionScanOnly {
		if scanned.Detected.Items > 0 {
			return DecisionInspect, "Potentially valuable data was found; this rule is report-only and cannot be deleted by the tool."
		}
		return DecisionNoAction, "No data was detected for this report-only rule."
	}
	if scanned.Eligible.Items == 0 {
		return DecisionNoAction, "No candidates satisfy the rule's age and safety gates."
	}
	if scanned.Rule.Auto && scanned.Rule.Risk == cleanup.RiskSafe {
		return DecisionAutoClean, "Eligible low-risk candidates satisfy the compiled age gate; explicit apply confirmation is still required."
	}
	return DecisionReview, "Rebuildable or user-visible data is eligible, but the rule requires explicit review."
}
