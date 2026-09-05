package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/sraodev/oli/internal/cleanup"
)

const (
	CommandDashboard    = "dashboard"
	CommandScan         = "scan"
	CommandExplore      = "explore"
	CommandRecommend    = "recommend"
	CommandCapabilities = "capabilities"
	CommandClean        = "clean"
	CommandAuto         = "auto"
	CommandVersion      = "version"
	CommandHelp         = "help"

	ProfileAll      = "all"
	ProfileSafe     = "safe"
	ProfileBalanced = "balanced"
	ProfileReview   = "review"
)

// Options is the validated command-line contract. Destructive execution is
// possible only when both Apply and Yes are true.
type Options struct {
	Command string

	Listen string
	NoOpen bool

	Profile       string
	RuleIDs       []string
	JSON          bool
	Apply         bool
	Yes           bool
	Scope         string
	MinSizeMiB    int64
	OlderThanDays int
	Limit         int
}

// Parse validates args without writing usage text or exiting the process.
func Parse(args []string) (Options, error) {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		return Options{Command: CommandHelp}, nil
	}
	command := CommandHelp
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}

	switch command {
	case CommandDashboard:
		return parseCommand(args, parseDashboard)
	case CommandScan:
		return parseCommand(args, parseScan)
	case CommandExplore:
		return parseCommand(args, parseExplore)
	case CommandRecommend:
		return parseCommand(args, parseRecommend)
	case CommandCapabilities:
		return parseCommand(args, parseCapabilities)
	case CommandClean:
		return parseCommand(args, parseClean)
	case CommandAuto:
		return parseCommand(args, parseAuto)
	case CommandVersion, CommandHelp:
		if len(args) != 0 {
			return Options{}, fmt.Errorf("%s does not accept arguments", command)
		}
		return Options{Command: command}, nil
	default:
		return Options{}, fmt.Errorf("unknown command %q", command)
	}
}

func parseCommand(args []string, parser func([]string) (Options, error)) (Options, error) {
	opts, err := parser(args)
	if errors.Is(err, flag.ErrHelp) {
		return Options{Command: CommandHelp}, nil
	}
	return opts, err
}

func parseDashboard(args []string) (Options, error) {
	opts := Options{
		Command: CommandDashboard,
		Listen:  "127.0.0.1:0",
	}
	fs := newFlagSet(CommandDashboard)
	fs.StringVar(&opts.Listen, "listen", opts.Listen, "loopback address for the local dashboard")
	fs.BoolVar(&opts.NoOpen, "no-open", false, "do not open the default browser")
	if err := parse(fs, args); err != nil {
		return Options{}, err
	}
	return opts, nil
}

func parseScan(args []string) (Options, error) {
	opts := Options{
		Command: CommandScan,
		Profile: ProfileAll,
	}
	fs := newFlagSet(CommandScan)
	var rawRules string
	fs.StringVar(&opts.Profile, "profile", opts.Profile, "scan profile: safe, balanced, review, or all")
	fs.StringVar(&rawRules, "rules", "", "comma-separated rule IDs (overrides profile selection)")
	fs.BoolVar(&opts.JSON, "json", false, "write machine-readable JSON")
	if err := parse(fs, args); err != nil {
		return Options{}, err
	}
	if err := validateProfile(opts.Profile); err != nil {
		return Options{}, err
	}
	rules, err := parseRuleIDs(rawRules)
	if err != nil {
		return Options{}, err
	}
	opts.RuleIDs = rules
	return opts, nil
}

func parseRecommend(args []string) (Options, error) {
	opts := Options{Command: CommandRecommend, Profile: ProfileAll}
	var rawRules string
	fs := newFlagSet(CommandRecommend)
	fs.StringVar(&opts.Profile, "profile", opts.Profile, "recommendation profile: safe, balanced, review, or all")
	fs.StringVar(&rawRules, "rules", "", "comma-separated rule IDs (overrides profile selection)")
	fs.BoolVar(&opts.JSON, "json", false, "write machine-readable JSON")
	if err := parse(fs, args); err != nil {
		return Options{}, err
	}
	if err := validateProfile(opts.Profile); err != nil {
		return Options{}, err
	}
	rules, err := parseRuleIDs(rawRules)
	if err != nil {
		return Options{}, err
	}
	opts.RuleIDs = rules
	return opts, nil
}

func parseCapabilities(args []string) (Options, error) {
	opts := Options{Command: CommandCapabilities}
	fs := newFlagSet(CommandCapabilities)
	fs.BoolVar(&opts.JSON, "json", false, "write machine-readable JSON")
	if err := parse(fs, args); err != nil {
		return Options{}, err
	}
	return opts, nil
}

func parseExplore(args []string) (Options, error) {
	opts := Options{Command: CommandExplore, Scope: "downloads", MinSizeMiB: 100, OlderThanDays: 180, Limit: 50}
	fs := newFlagSet(CommandExplore)
	fs.StringVar(&opts.Scope, "scope", opts.Scope, "downloads, documents, desktop, movies, music, pictures, applications, or all")
	fs.Int64Var(&opts.MinSizeMiB, "min-size-mib", opts.MinSizeMiB, "large-file threshold in MiB")
	fs.IntVar(&opts.OlderThanDays, "older-than-days", opts.OlderThanDays, "days since modification (not last use)")
	fs.IntVar(&opts.Limit, "limit", opts.Limit, "maximum entries per list (1–200)")
	fs.BoolVar(&opts.JSON, "json", false, "write a read-only storage report")
	if err := parse(fs, args); err != nil {
		return Options{}, err
	}
	if !cleanup.ValidExploreScope(opts.Scope) {
		return Options{}, fmt.Errorf("unknown exploration scope %q", opts.Scope)
	}
	if opts.MinSizeMiB < 1 || opts.MinSizeMiB > 1048576 || opts.OlderThanDays < 1 || opts.OlderThanDays > 36500 || opts.Limit < 1 || opts.Limit > 200 {
		return Options{}, errors.New("explore requires size 1–1048576 MiB, age 1–36500 days, and limit 1–200")
	}
	return opts, nil
}

func parseClean(args []string) (Options, error) {
	opts := Options{
		Command: CommandClean,
		Profile: ProfileSafe,
	}
	var rawRules string
	fs := newFlagSet(CommandClean)
	fs.StringVar(&opts.Profile, "profile", opts.Profile, "cleanup profile: safe, balanced, review, or all")
	fs.StringVar(&rawRules, "rules", "", "comma-separated rule IDs (overrides profile selection)")
	addExecutionFlags(fs, &opts)
	if err := parse(fs, args); err != nil {
		return Options{}, err
	}
	if err := validateProfile(opts.Profile); err != nil {
		return Options{}, err
	}
	rules, err := parseRuleIDs(rawRules)
	if err != nil {
		return Options{}, err
	}
	opts.RuleIDs = rules
	if err := validateExecution(opts); err != nil {
		return Options{}, err
	}
	return opts, nil
}

func parseAuto(args []string) (Options, error) {
	opts := Options{
		Command: CommandAuto,
		Profile: ProfileSafe,
	}
	fs := newFlagSet(CommandAuto)
	addExecutionFlags(fs, &opts)
	if err := parse(fs, args); err != nil {
		return Options{}, err
	}
	if err := validateExecution(opts); err != nil {
		return Options{}, err
	}
	return opts, nil
}

func addExecutionFlags(fs *flag.FlagSet, opts *Options) {
	fs.BoolVar(&opts.JSON, "json", false, "write machine-readable JSON")
	fs.BoolVar(&opts.Apply, "apply", false, "scan and apply a fresh cleanup plan")
	fs.BoolVar(&opts.Yes, "yes", false, "authorize noninteractive permanent deletion")
}

func validateExecution(opts Options) error {
	if opts.Apply && !opts.Yes {
		return errors.New("--apply requires --yes")
	}
	if opts.Yes && !opts.Apply {
		return errors.New("--yes requires --apply")
	}
	return nil
}

func validateProfile(profile string) error {
	if profile != ProfileAll && profile != ProfileSafe && profile != ProfileBalanced && profile != ProfileReview {
		return fmt.Errorf("unknown profile %q (want safe, balanced, review, or all)", profile)
	}
	return nil
}

func parseRuleIDs(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	seen := make(map[string]struct{})
	var ids []string
	for _, raw := range strings.Split(value, ",") {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil, errors.New("--rules contains an empty rule ID")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return nil
}

// Usage is intentionally compact so it remains useful in both terminals and
// issue reports.
func Usage() string {
	return `Oli — Open Lifecycle Intelligence
Your machine's housekeeper. Safe, preview-first macOS cleanup.

Usage:
  oli dashboard [--listen 127.0.0.1:0] [--no-open]
  oli capabilities [--json]
  oli explore [--scope downloads|documents|desktop|movies|music|pictures|applications|all] [--min-size-mib 100] [--older-than-days 180] [--limit 50] [--json]
  oli scan [--profile safe|balanced|review|all] [--rules id,...] [--json]
  oli recommend [--profile safe|balanced|review|all] [--rules id,...] [--json]
  oli clean [--profile safe|balanced|review|all] [--rules id,...] [--apply --yes] [--json]
  oli auto [--apply --yes] [--json]
  oli version

Cleanup commands are dry runs unless both --apply and --yes are present.
`
}
