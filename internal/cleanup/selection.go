package cleanup

import "fmt"

// PreviewSelection validates against the private plan, not mutable public scan
// fields. Metrics deduplicate hard links across the exact selected candidates.
func (e *Engine) PreviewSelection(scan *Scan, opts CleanOptions) (*Selection, error) {
	if scan == nil || scan.plan == nil || scan.plan.engineToken != e.token {
		return nil, ErrInvalidScan
	}
	scan.plan.mu.Lock()
	defer scan.plan.mu.Unlock()
	return e.selectCandidates(scan.plan, opts)
}

// Caller holds plan.mu. Rule selection remains supported for existing bulk CLI
// workflows; candidate selection cannot be combined with it or fall back to it.
func (e *Engine) selectCandidates(plan *frozenPlan, opts CleanOptions) (*Selection, error) {
	if plan.consumed {
		return nil, fmt.Errorf("%w: scan has already been used", ErrInvalidScan)
	}
	wanted := make(map[string]bool)
	rules := make(map[string]bool)
	if opts.CandidateIDs != nil {
		if len(opts.CandidateIDs) == 0 || len(opts.RuleIDs) != 0 {
			return nil, fmt.Errorf("%w: select candidates or rules, never both or an empty candidate list", ErrSelection)
		}
		for _, id := range opts.CandidateIDs {
			if id == "" || wanted[id] {
				return nil, fmt.Errorf("%w: empty or duplicate candidate ID", ErrSelection)
			}
			wanted[id] = true
		}
	} else {
		selected, err := e.selectedRules(opts.RuleIDs, false)
		if err != nil {
			return nil, err
		}
		for _, compiled := range selected {
			rule, ok := plan.rules[compiled.rule.ID]
			if !ok {
				return nil, fmt.Errorf("%w: rule %q was not included in scan", ErrSelection, compiled.rule.ID)
			}
			if rule.action == ActionScanOnly {
				return nil, fmt.Errorf("%w: %s", ErrScanOnly, compiled.rule.ID)
			}
			rules[compiled.rule.ID] = true
		}
	}
	selection := &Selection{ScanID: plan.scanID, RuleIDs: []string{}, CandidateIDs: []string{}, Candidates: []Candidate{}}
	var selected []*frozenCandidate
	for _, id := range plan.order {
		rule := plan.rules[id]
		included := rules[id]
		for _, candidate := range rule.candidates {
			if opts.CandidateIDs != nil {
				if !wanted[candidate.summary.ID] {
					continue
				}
				if !candidate.summary.Eligible || rule.action != ActionClean {
					return nil, fmt.Errorf("%w: candidate is not eligible", ErrSelection)
				}
				delete(wanted, candidate.summary.ID)
			} else if !rules[id] || !candidate.summary.Eligible {
				continue
			}
			included = true
			selection.CandidateIDs = append(selection.CandidateIDs, candidate.summary.ID)
			selection.Candidates = append(selection.Candidates, candidate.summary)
			selected = append(selected, candidate)
		}
		if included {
			selection.RuleIDs = append(selection.RuleIDs, id)
		}
	}
	if len(wanted) != 0 {
		return nil, fmt.Errorf("%w: unknown or foreign-scan candidate ID", ErrSelection)
	}
	selection.Metrics = metricsForCandidates(selected, false)
	return selection, nil
}
