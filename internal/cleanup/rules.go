package cleanup

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	RuleUserCaches      = "user-caches"
	RuleUserLogs        = "user-logs"
	RuleXcodeDerived    = "xcode-derived-data"
	RuleDeveloperCaches = "developer-caches"
	RuleTrash           = "trash"
	RuleXcodeArchives   = "xcode-archives"
	RuleDeviceBackups   = "ios-device-backups"
)

// DefaultRules returns the intentionally narrow built-in macOS rules. Missing
// roots are expected and silently ignored during scanning.
func DefaultRules(home string) []Rule {
	join := func(parts ...string) string {
		all := append([]string{home}, parts...)
		return filepath.Join(all...)
	}

	return []Rule{
		{
			ID: RuleUserCaches, Name: "Application caches",
			Description: "Direct children of ~/Library/Caches not used for at least 30 days.",
			Risk:        RiskSafe, Action: ActionClean, Default: true, Auto: true,
			MinimumAge: 30 * 24 * time.Hour,
			Roots:      []string{join("Library", "Caches")},
		},
		{
			ID: RuleUserLogs, Name: "Application logs",
			Description: "Direct children of ~/Library/Logs not updated for at least 30 days.",
			Risk:        RiskSafe, Action: ActionClean, Default: true, Auto: true,
			MinimumAge: 30 * 24 * time.Hour,
			Roots:      []string{join("Library", "Logs")},
		},
		{
			ID: RuleXcodeDerived, Name: "Xcode DerivedData",
			Description: "Derived build data not updated for at least 7 days.",
			Risk:        RiskSafe, Action: ActionClean, Default: true, Auto: true,
			MinimumAge: 7 * 24 * time.Hour,
			Roots:      []string{join("Library", "Developer", "Xcode", "DerivedData")},
		},
		{
			ID: RuleDeveloperCaches, Name: "Developer caches",
			Description: "Gradle, npm, and Go module download caches not updated for at least 30 days.",
			Risk:        RiskCaution, Action: ActionClean, MinimumAge: 30 * 24 * time.Hour,
			Roots: []string{
				join(".gradle", "caches"),
				join(".npm", "_cacache"),
				join("go", "pkg", "mod", "cache"),
			},
		},
		{
			ID: RuleTrash, Name: "Trash",
			Description: "Items currently in the user's Trash.",
			Risk:        RiskCaution, Action: ActionClean,
			Roots: []string{join(".Trash")},
		},
		{
			ID: RuleXcodeArchives, Name: "Xcode archives",
			Description: "Xcode archives are reported only and are never deleted.",
			Risk:        RiskReview, Action: ActionScanOnly,
			Roots: []string{join("Library", "Developer", "Xcode", "Archives")},
		},
		{
			ID: RuleDeviceBackups, Name: "iOS device backups",
			Description: "MobileSync backups are reported only and are never deleted.",
			Risk:        RiskReview, Action: ActionScanOnly,
			Roots: []string{join("Library", "Application Support", "MobileSync", "Backup")},
		},
	}
}

type compiledRule struct {
	rule  Rule
	info  RuleInfo
	roots []compiledRoot
}

type compiledRoot struct {
	path    string
	display string
}

func compileRules(home string, rules []Rule) ([]compiledRule, error) {
	seenIDs := make(map[string]struct{}, len(rules))
	var compiled []compiledRule
	var allRoots []string
	for _, source := range rules {
		rule := cloneRule(source)
		if err := validateRule(rule); err != nil {
			return nil, err
		}
		if _, ok := seenIDs[rule.ID]; ok {
			return nil, fmt.Errorf("cleanup: duplicate rule ID %q", rule.ID)
		}
		seenIDs[rule.ID] = struct{}{}

		cr := compiledRule{rule: rule}
		for _, root := range rule.Roots {
			if !filepath.IsAbs(root) {
				root = filepath.Join(home, root)
			}
			root = filepath.Clean(root)
			if !strictDescendant(home, root) {
				return nil, fmt.Errorf("cleanup: rule %q root must be a strict descendant of home: %q", rule.ID, root)
			}
			for _, other := range allRoots {
				if pathsOverlap(root, other) {
					return nil, fmt.Errorf("cleanup: overlapping rule roots %q and %q", root, other)
				}
			}
			allRoots = append(allRoots, root)
			cr.roots = append(cr.roots, compiledRoot{path: root, display: displayPath(home, root)})
		}
		cr.info = ruleInfo(rule, cr.roots)
		compiled = append(compiled, cr)
	}
	return compiled, nil
}

func validateRule(rule Rule) error {
	if strings.TrimSpace(rule.ID) == "" || strings.TrimSpace(rule.Name) == "" {
		return fmt.Errorf("cleanup: rule ID and name are required")
	}
	if strings.ContainsAny(rule.ID, " /\\") {
		return fmt.Errorf("cleanup: invalid rule ID %q", rule.ID)
	}
	if rule.Risk != RiskSafe && rule.Risk != RiskCaution && rule.Risk != RiskReview {
		return fmt.Errorf("cleanup: rule %q has invalid risk %q", rule.ID, rule.Risk)
	}
	if rule.Action != ActionClean && rule.Action != ActionScanOnly {
		return fmt.Errorf("cleanup: rule %q has invalid action %q", rule.ID, rule.Action)
	}
	if len(rule.Roots) == 0 {
		return fmt.Errorf("cleanup: rule %q has no roots", rule.ID)
	}
	if rule.MinimumAge < 0 {
		return fmt.Errorf("cleanup: rule %q has a negative minimum age", rule.ID)
	}
	if rule.Auto && (rule.Action != ActionClean || rule.MinimumAge == 0) {
		return fmt.Errorf("cleanup: automatic rule %q must be cleanable and age-gated", rule.ID)
	}
	if rule.Action == ActionScanOnly && (rule.Auto || rule.Default) {
		return fmt.Errorf("cleanup: scan-only rule %q cannot be automatic or selected by default", rule.ID)
	}
	return nil
}

func cloneRule(rule Rule) Rule {
	rule.Roots = append([]string(nil), rule.Roots...)
	return rule
}

func ruleInfo(rule Rule, roots []compiledRoot) RuleInfo {
	displays := make([]string, len(roots))
	for i, root := range roots {
		displays[i] = root.display
	}
	return RuleInfo{
		ID: rule.ID, Name: rule.Name, Description: rule.Description,
		Risk: rule.Risk, Action: rule.Action, Default: rule.Default, Auto: rule.Auto,
		MinimumAgeSeconds: int64(rule.MinimumAge / time.Second), Roots: displays,
	}
}

func strictDescendant(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && rel != "" && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pathsOverlap(a, b string) bool {
	return a == b || strictDescendant(a, b) || strictDescendant(b, a)
}

func displayPath(home, path string) string {
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "~"
	}
	return "~/" + filepath.ToSlash(rel)
}
