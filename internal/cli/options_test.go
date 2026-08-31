package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseDefaultsToHelp(t *testing.T) {
	opts, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Command != CommandHelp {
		t.Fatalf("unexpected options: %#v", opts)
	}
}

func TestParseDashboardIsExplicit(t *testing.T) {
	opts, err := Parse([]string{"dashboard"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Command != CommandDashboard || opts.Listen != "127.0.0.1:0" {
		t.Fatalf("unexpected options: %#v", opts)
	}
}

func TestParseCleanIsDryRunByDefault(t *testing.T) {
	opts, err := Parse([]string{"clean"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Apply || opts.Yes || opts.Profile != ProfileSafe {
		t.Fatalf("unexpected options: %#v", opts)
	}
}

func TestParseCleanRequiresBothExecutionFlags(t *testing.T) {
	for _, args := range [][]string{
		{"clean", "--apply"},
		{"clean", "--yes"},
		{"auto", "--apply"},
		{"auto", "--yes"},
		{"clean", "--interactive", "--apply"},
		{"clean", "--interactive", "--yes"},
		{"auto", "--interactive"},
	} {
		if _, err := Parse(args); err == nil {
			t.Fatalf("Parse(%q) succeeded, want error", args)
		}
	}
}

func TestParseCleanDeduplicatesRules(t *testing.T) {
	opts, err := Parse([]string{"clean", "--rules", "logs, caches,logs"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"logs", "caches"}
	if !reflect.DeepEqual(opts.RuleIDs, want) {
		t.Fatalf("rules = %q, want %q", opts.RuleIDs, want)
	}
}

func TestParseRejectsUnknownProfile(t *testing.T) {
	_, err := Parse([]string{"scan", "--profile", "aggressive"})
	if err == nil || !strings.Contains(err.Error(), "unknown profile") {
		t.Fatalf("error = %v, want unknown profile", err)
	}
}

func TestParseScanAndRecommendAcceptAgentSelections(t *testing.T) {
	for _, command := range []string{CommandScan, CommandRecommend} {
		opts, err := Parse([]string{command, "--profile", ProfileReview, "--rules", "backup,cache", "--json"})
		if err != nil {
			t.Fatalf("Parse(%q): %v", command, err)
		}
		if opts.Command != command || opts.Profile != ProfileReview || !opts.JSON {
			t.Fatalf("unexpected options: %#v", opts)
		}
		if want := []string{"backup", "cache"}; !reflect.DeepEqual(opts.RuleIDs, want) {
			t.Fatalf("rules = %q, want %q", opts.RuleIDs, want)
		}
	}
}

func TestParseCapabilitiesJSON(t *testing.T) {
	opts, err := Parse([]string{"capabilities", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Command != CommandCapabilities || !opts.JSON {
		t.Fatalf("unexpected options: %#v", opts)
	}
}

func TestParseRejectsUnexpectedArguments(t *testing.T) {
	_, err := Parse([]string{"scan", "extra"})
	if err == nil || !strings.Contains(err.Error(), "unexpected argument") {
		t.Fatalf("error = %v, want unexpected argument", err)
	}
}

func TestParseHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"scan", "--help"}} {
		opts, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse(%q): %v", args, err)
		}
		if opts.Command != CommandHelp {
			t.Fatalf("Parse(%q) command = %q, want help", args, opts.Command)
		}
	}
}

func TestExploreDefaultsAndUnsafeInputs(t *testing.T) {
	opts, err := Parse([]string{"explore", "--json"})
	if err != nil || opts.Scope != "downloads" || opts.MinSizeMiB != 100 || opts.Limit != 50 || !opts.JSON {
		t.Fatalf("opts=%+v err=%v", opts, err)
	}
	for _, args := range [][]string{
		{"explore", "--apply", "--yes"}, {"explore", "--scope", "../../"},
		{"explore", "--limit", "201"}, {"explore", "--min-size-mib", "-1"},
		{"explore", "--older-than-days", "0"}, {"explore", "/tmp"},
	} {
		if _, err := Parse(args); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
}
