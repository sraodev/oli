package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExclusionsNarrowScanAndFreshCleanup(t *testing.T) {
	for _, when := range []string{"scan", "before-clean", "between-candidates"} {
		t.Run(when, func(t *testing.T) {
			e, scan, root := selectionFixture(t)
			protect := func() { mustWrite(t, filepath.Join(e.home, ProtectionConfig), `{"version":1,"paths":["cache/b"]}`) }
			if when == "scan" {
				protect()
				var err error
				scan, err = e.Scan(context.Background(), ScanOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if scan.Rules[0].Candidates[1].Eligible {
					t.Fatal("protected candidate eligible")
				}
			}
			if when == "before-clean" {
				protect()
			}
			result, err := e.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{"cache"}, Callback: func(event Event) {
				if when == "between-candidates" && event.Kind == EventCandidateRemoved {
					protect()
				}
			}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Removed) != 2 {
				t.Fatalf("result=%+v", result)
			}
			if _, err := os.Stat(filepath.Join(root, "b")); err != nil {
				t.Fatal("protected item removed")
			}
		})
	}
}

func TestExclusionsNestedRuleAndCaseAlias(t *testing.T) {
	for _, policy := range []string{`{"version":1,"paths":["cache/app/keep"]}`, `{"version":1,"paths":["cache/app/future"]}`, `{"version":1,"rules":["cache"]}`, `{"version":1,"paths":["CACHE"]}`} {
		home := t.TempDir()
		root := filepath.Join(home, "cache")
		mustWrite(t, filepath.Join(root, "app", "keep"), "protected")
		if strings.Contains(policy, "CACHE") {
			if _, err := os.Stat(filepath.Join(home, "CACHE")); os.IsNotExist(err) {
				continue
			}
		}
		mustWrite(t, filepath.Join(home, ProtectionConfig), policy)
		e := mustEngine(t, home, testRule("cache", root))
		scan, err := e.Scan(context.Background(), ScanOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if scan.Rules[0].Candidates[0].Eligible || scan.Eligible.LogicalBytes != 0 {
			t.Fatal("excluded tree eligible")
		}
	}
}

func TestExclusionsHardlinkAndNativeUnicodeAliases(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	mustWrite(t, filepath.Join(root, "é"), "keep")
	mustWrite(t, filepath.Join(home, "outside-rule", "placeholder"), "keep")
	alias := filepath.Join(home, "outside-rule", "alias")
	if err := os.Link(filepath.Join(root, "é"), alias); err != nil {
		t.Fatal(err)
	}
	e := mustEngine(t, home, testRule("cache", root))
	policies := []string{`{"version":1,"paths":["outside-rule/alias"]}`}
	if _, err := os.Stat(filepath.Join(root, "e\u0301")); err == nil {
		policies = append(policies, `{"version":1,"paths":["cache/e\u0301"]}`)
	}
	for _, policy := range policies {
		mustWrite(t, filepath.Join(home, ProtectionConfig), policy)
		scan, err := e.Scan(context.Background(), ScanOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if scan.Rules[0].Candidates[0].Eligible {
			t.Fatalf("alias policy did not protect %s", policy)
		}
		// Removing an exclusion never expands an already frozen plan.
		mustWrite(t, filepath.Join(home, ProtectionConfig), `{"version":1}`)
		result, err := e.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{"cache"}})
		if err != nil || len(result.Removed) != 0 {
			t.Fatalf("frozen protection expanded: %+v %v", result, err)
		}
	}
}

func TestExclusionsFailClosed(t *testing.T) {
	for _, policy := range []string{`null`, `{`, `{"version":2}`, `{"version":1,"paths":["../outside"]}`, `{"version":1,"paths":["/absolute"]}`, `{"version":1,"roots":["cache"]}`, `{"version":1,"rules":["unknown"]}`, `{"version":1,"paths":["cache/a"],"paths":[]}`, `{"version":1} {}`, strings.Repeat(" ", 16385)} {
		e, scan, root := selectionFixture(t)
		mustWrite(t, filepath.Join(e.home, ProtectionConfig), policy)
		if _, err := e.Scan(context.Background(), ScanOptions{}); err != ErrProtection {
			t.Fatalf("policy=%q err=%v", policy, err)
		}
		if _, err := e.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{"cache"}}); err != ErrProtection {
			t.Fatalf("clean err=%v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "a")); err != nil {
			t.Fatal("invalid policy permitted deletion")
		}
	}
}

func TestProtectionUnreadableOrSymlinkedConfig(t *testing.T) {
	e, _, _ := selectionFixture(t)
	config := filepath.Join(e.home, ProtectionConfig)
	mustWrite(t, config, `{"version":1}`)
	if err := os.Chmod(config, 0000); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Scan(context.Background(), ScanOptions{}); err != ErrProtection {
		t.Fatalf("unreadable policy: %v", err)
	}
	if err := os.Chmod(config, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(e.home, "cache", "a"), config); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Scan(context.Background(), ScanOptions{}); err != ErrProtection {
		t.Fatalf("symlink policy: %v", err)
	}
}

func TestExclusionsRejectSymlinkAndRenamedPlan(t *testing.T) {
	e, scan, root := selectionFixture(t)
	if err := os.Symlink("a", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(e.home, ProtectionConfig), `{"version":1,"paths":["cache/alias"]}`)
	if _, err := e.Scan(context.Background(), ScanOptions{}); err != ErrProtection {
		t.Fatalf("alias err=%v", err)
	}
	mustWrite(t, filepath.Join(e.home, ProtectionConfig), `{"version":1,"paths":["cache/a"]}`)
	if err := os.Rename(filepath.Join(root, "a"), filepath.Join(root, "renamed")); err != nil {
		t.Fatal(err)
	}
	result, err := e.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{"cache"}})
	if err != nil || len(result.Removed) != 0 {
		t.Fatalf("renamed plan=%+v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed")); err != nil {
		t.Fatal("renamed object removed")
	}
}

func TestScanBoundsAreExplicitPartialCoverage(t *testing.T) {
	e, _, _ := selectionFixture(t)
	for _, opts := range []ScanOptions{{entryLimit: 1}, {candidateLimit: 1}} {
		scan, err := e.Scan(context.Background(), opts)
		if err != nil || !scan.Partial || len(scan.Warnings) == 0 || len(scan.Rules[0].Candidates) != 1 {
			t.Fatalf("scan=%+v err=%v", scan, err)
		}
	}
}
