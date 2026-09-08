package cleanup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExpandedCatalogCleanupBoundaries(t *testing.T) {
	home := t.TempDir()
	engine, err := NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-40 * 24 * time.Hour)
	var retained []string
	for _, rel := range []string{
		"Library/Caches/pypoetry/virtualenvs/project/bin/python",
		"Library/Application Support/minecraft/saves/world",
		"Library/Application Support/Steam/steamapps/common/game",
		".android/avd/device/userdata", ".android/adbkey", ".pyenv/versions/project/bin/python",
		".lunarclient/settings/config", ".cacher/snippets/work", ".kite/config",
		"Downloads/personal", ".wget-hsts",
	} {
		path := filepath.Join(home, rel)
		mustWrite(t, path, "preserve")
		retained = append(retained, path)
	}
	var cleanIDs []string
	for _, rule := range DefaultRules(home) {
		if rule.ID != RuleAppLogs && rule.ID != RuleDeveloperCaches && rule.ID != RuleAppCaches && rule.ID != RuleMobilePackages {
			continue
		}
		if rule.Auto || rule.Default {
			t.Fatalf("expanded rule is automatic: %s", rule.ID)
		}
		if rule.Action == ActionClean {
			cleanIDs = append(cleanIDs, rule.ID)
		}
		for _, root := range rule.Roots {
			stale := filepath.Join(root, "old")
			mustWrite(t, stale, "regenerable")
			if err := os.Chtimes(stale, old, old); err != nil {
				t.Fatal(err)
			}
			fresh := filepath.Join(root, "fresh")
			mustWrite(t, fresh, "active")
			retained = append(retained, fresh)
			if rule.Action == ActionScanOnly {
				retained = append(retained, stale)
			}
		}
	}
	scan, err := engine.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	foundExclusion := false
	for _, warning := range scan.Warnings {
		if warning.Code == "protected_child" && warning.DisplayPath == "~/Library/Caches/pypoetry" {
			foundExclusion = true
		}
	}
	if !foundExclusion {
		t.Fatal("Poetry exclusion was not reported")
	}
	for _, id := range []string{RuleAppCaches, RuleMobilePackages, RuleDeviceBackups, RuleXcodeArchives} {
		if _, err := engine.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{id}}); !errors.Is(err, ErrScanOnly) {
			t.Fatalf("%s was not blocked: %v", id, err)
		}
	}
	result, err := engine.Clean(context.Background(), scan, CleanOptions{RuleIDs: cleanIDs})
	if err != nil || len(result.Removed) != 11 || len(result.Failures) != 0 || len(result.Rejected) != 0 {
		t.Fatalf("cleanup: %+v, %v", result, err)
	}
	for _, path := range retained {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("protected/fresh file lost: %s: %v", path, err)
		}
	}
}

func TestExcludedChildrenAreFrozenAndValidated(t *testing.T) {
	home := t.TempDir()
	rules := DefaultRules(home)
	engine, err := New(home, rules)
	if err != nil {
		t.Fatal(err)
	}
	rules[0].ExcludedChildren[0] = "changed"
	infos := engine.Rules()
	infos[0].ExcludedChildren[0] = "also-changed"
	if engine.Rules()[0].ExcludedChildren[0] != "pypoetry" {
		t.Fatal("caller could mutate compiled exclusions")
	}
	for _, name := range []string{"", ".", "..", "nested/path", `nested\path`} {
		rules = DefaultRules(home)
		rules[0].ExcludedChildren = []string{name}
		if _, err := New(home, rules); err == nil {
			t.Fatalf("invalid exclusion accepted: %q", name)
		}
	}
}

func TestPoetryExclusionIgnoresCaseAndCreatesNoCleanupAuthority(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, "Library", "Caches", "PyPoEtRy", "virtualenvs", "python")
	mustWrite(t, file, "environment")
	engine, err := NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	// Even a far-future age cutoff cannot make a protected tree cleanable.
	scan, err := engine.Scan(context.Background(), ScanOptions{RuleIDs: []string{RuleUserCaches}, Now: time.Now().Add(365 * 24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Rules[0].Candidates) != 0 || scan.Detected.LogicalBytes != 0 || len(scan.Warnings) != 1 || scan.Warnings[0].Code != "protected_child" {
		t.Fatalf("excluded data entered the plan: %+v", scan)
	}
	if _, err := engine.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{RuleUserCaches}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("protected environment was removed")
	}
}
