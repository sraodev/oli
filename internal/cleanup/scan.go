package cleanup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	errRootMissing = errors.New("cleanup: root is missing")
	errRootSymlink = errors.New("cleanup: root path contains a symlink")
)

// Scan builds a frozen cleanup plan. An empty RuleIDs filter scans every rule.
func (e *Engine) Scan(ctx context.Context, opts ScanOptions) (*Scan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rules, err := e.selectedRules(opts.RuleIDs, true)
	if err != nil {
		return nil, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	id, err := newScanID()
	if err != nil {
		return nil, fmt.Errorf("cleanup: create scan ID: %w", err)
	}
	scan := &Scan{ID: id, StartedAt: time.Now()}
	plan := &frozenPlan{engineToken: e.token, scanID: id, rules: make(map[string]*frozenRule, len(rules))}
	scan.plan = plan
	emit(opts.Callback, Event{Kind: EventScanStarted})

	for _, rule := range rules {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		emit(opts.Callback, Event{Kind: EventRuleStarted, RuleID: rule.rule.ID})
		frozen := &frozenRule{
			info: cloneRuleInfo(rule.info), action: rule.rule.Action, auto: rule.rule.Auto,
			roots: make(map[string]frozenRoot),
		}
		result := RuleScan{Rule: cloneRuleInfo(rule.info)}

		addWarning := func(warning Warning) {
			result.Warnings = append(result.Warnings, warning)
			scan.Warnings = append(scan.Warnings, warning)
			payload := warning
			emit(opts.Callback, Event{
				Kind: EventWarning, RuleID: warning.RuleID,
				DisplayPath: warning.DisplayPath, Message: warning.Message, Warning: &payload,
			})
		}

		for _, root := range rule.roots {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			rootInfo, rootFP, err := e.inspectRoot(root.path)
			if errors.Is(err, errRootMissing) {
				continue
			}
			if err != nil {
				code := "root_unavailable"
				if errors.Is(err, errRootSymlink) {
					code = "root_symlink"
				}
				addWarning(Warning{
					RuleID: rule.rule.ID, DisplayPath: root.display, Code: code,
					Message: fmt.Sprintf("Skipped %s: %s", root.display, publicError(e.home, err)),
				})
				continue
			}
			if !rootInfo.IsDir() {
				addWarning(Warning{
					RuleID: rule.rule.ID, DisplayPath: root.display, Code: "root_not_directory",
					Message: fmt.Sprintf("Skipped %s because it is not a directory", root.display),
				})
				continue
			}

			children, err := readDirNoFollow(root.path, rootFP)
			if err != nil {
				addWarning(Warning{
					RuleID: rule.rule.ID, DisplayPath: root.display, Code: "root_unreadable",
					Message: fmt.Sprintf("Skipped %s: %s", root.display, publicError(e.home, err)),
				})
				continue
			}
			sortDirEntries(children)
			var rootCandidates []*frozenCandidate
			for _, child := range children {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				path := filepath.Join(root.path, child.Name())
				candidate, err := walkCandidate(ctx, e.home, root.path, path, rootFP.Device)
				if err != nil {
					addWarning(Warning{
						RuleID: rule.rule.ID, DisplayPath: displayPath(e.home, path), Code: "candidate_unavailable",
						Message: fmt.Sprintf("Skipped %s: %s", displayPath(e.home, path), publicError(e.home, err)),
					})
					continue
				}
				applyUniqueBytes(candidate, make(map[string]struct{}))
				finishCandidate(candidate, rule, root, now, e.home)
				rootCandidates = append(rootCandidates, candidate)
			}

			_, currentRoot, err := inspect(root.path)
			if err != nil || !rootFP.equal(currentRoot) {
				addWarning(Warning{
					RuleID: rule.rule.ID, DisplayPath: root.display, Code: "root_changed",
					Message: fmt.Sprintf("Skipped %s because it changed during the scan", root.display),
				})
				continue
			}
			frozen.roots[root.path] = frozenRoot{path: root.path, display: root.display, fingerprint: rootFP}
			for _, candidate := range rootCandidates {
				frozen.candidates = append(frozen.candidates, candidate)
				result.Candidates = append(result.Candidates, candidate.summary)
				payload := cloneCandidate(candidate.summary)
				emit(opts.Callback, Event{
					Kind: EventCandidateScanned, RuleID: rule.rule.ID,
					CandidateID: payload.ID, DisplayPath: payload.DisplayPath, Candidate: &payload,
				})
			}
		}

		result.Detected = metricsForCandidates(frozen.candidates, false)
		result.Eligible = metricsForCandidates(frozen.candidates, true)
		plan.rules[rule.rule.ID] = frozen
		plan.order = append(plan.order, rule.rule.ID)
		scan.Rules = append(scan.Rules, result)
		payload := cloneRuleScan(result)
		emit(opts.Callback, Event{Kind: EventRuleCompleted, RuleID: rule.rule.ID, Rule: &payload})
	}

	var all []*frozenCandidate
	for _, id := range plan.order {
		all = append(all, plan.rules[id].candidates...)
	}
	scan.Detected = metricsForCandidates(all, false)
	scan.Eligible = metricsForCandidates(all, true)
	scan.CompletedAt = time.Now()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	emit(opts.Callback, Event{Kind: EventScanCompleted})
	return scan, nil
}

func (e *Engine) inspectRoot(path string) (os.FileInfo, fingerprint, error) {
	rel, err := filepath.Rel(e.home, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fingerprint{}, fmt.Errorf("root is outside the compiled home")
	}
	current := e.home
	components := append([]string{"."}, strings.Split(rel, string(filepath.Separator))...)
	var info os.FileInfo
	var fp fingerprint
	var homeDevice uint64
	for i, component := range components {
		if component != "." {
			current = filepath.Join(current, component)
		}
		info, fp, err = inspect(current)
		if os.IsNotExist(err) {
			return nil, fingerprint{}, errRootMissing
		}
		if err != nil {
			return nil, fingerprint{}, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fingerprint{}, errRootSymlink
		}
		if i == 0 {
			if !e.homeFingerprint.sameIdentity(fp) {
				return nil, fingerprint{}, fmt.Errorf("home directory identity changed")
			}
			homeDevice = fp.Device
		} else if fp.HasIdentity && e.homeFingerprint.HasIdentity && fp.Device != homeDevice {
			return nil, fingerprint{}, fmt.Errorf("root crosses a filesystem boundary")
		}
		if i < len(components)-1 && !info.IsDir() {
			return nil, fingerprint{}, fmt.Errorf("root path component is not a directory")
		}
	}
	return info, fp, nil
}

func finishCandidate(candidate *frozenCandidate, rule compiledRule, root compiledRoot, now time.Time, home string) {
	top := candidate.entries[0]
	digest := manifestDigest(home, rule.rule.ID, candidate.entries)
	eligible := rule.rule.Action == ActionClean
	reason := "selected manually"
	if rule.rule.Action == ActionScanOnly {
		eligible = false
		reason = "scan-only rule"
	} else if rule.rule.MinimumAge > 0 {
		cutoff := now.Add(-rule.rule.MinimumAge)
		eligible = !candidate.summary.Metrics.LatestModified.After(cutoff)
		if eligible {
			reason = fmt.Sprintf("not modified for at least %s", formatAge(rule.rule.MinimumAge))
		} else {
			reason = fmt.Sprintf("modified within the last %s", formatAge(rule.rule.MinimumAge))
		}
	}
	candidate.summary.ID = rule.rule.ID + "-" + digest[:16]
	candidate.summary.RuleID = rule.rule.ID
	candidate.summary.DisplayPath = top.display
	candidate.summary.RootDisplayPath = root.display
	candidate.summary.Kind = top.kind
	candidate.summary.Fingerprint = digest
	candidate.summary.Modified = time.Unix(0, top.fingerprint.ModifiedNano)
	candidate.summary.LatestModified = candidate.summary.Metrics.LatestModified
	candidate.summary.Eligible = eligible
	candidate.summary.AutoEligible = eligible && rule.rule.Auto
	candidate.summary.EligibilityReason = reason
}

func formatAge(age time.Duration) string {
	if age%(24*time.Hour) == 0 {
		return fmt.Sprintf("%d days", int(age/(24*time.Hour)))
	}
	return age.String()
}

func metricsForCandidates(candidates []*frozenCandidate, eligibleOnly bool) Metrics {
	seen := make(map[string]struct{})
	var metrics Metrics
	for _, candidate := range candidates {
		if eligibleOnly && !candidate.summary.Eligible {
			continue
		}
		for _, entry := range candidate.entries {
			countEntry(&metrics, entry)
			if entry.kind != "file" {
				continue
			}
			key := inodeKey(entry.path, entry.fingerprint)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			metrics.UniqueFiles++
			if entry.fingerprint.Size > 0 {
				metrics.LogicalBytes = addInt64(metrics.LogicalBytes, entry.fingerprint.Size)
			}
			metrics.AllocatedBytes = addInt64(metrics.AllocatedBytes, allocatedBytes(entry.fingerprint.Blocks))
		}
	}
	return metrics
}

func cloneCandidate(candidate Candidate) Candidate {
	return candidate
}

func cloneRuleScan(scan RuleScan) RuleScan {
	scan.Rule = cloneRuleInfo(scan.Rule)
	scan.Candidates = append([]Candidate(nil), scan.Candidates...)
	scan.Warnings = append([]Warning(nil), scan.Warnings...)
	return scan
}
