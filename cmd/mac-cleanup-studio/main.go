package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/sraodev/mac-cleanup-studio/internal/app"
	"github.com/sraodev/mac-cleanup-studio/internal/cleanup"
	"github.com/sraodev/mac-cleanup-studio/internal/cli"
	"github.com/sraodev/mac-cleanup-studio/internal/dashboard"
)

var (
	version       = "dev"
	commit        = "unknown"
	buildDate     = "unknown"
	buildIdentity = "development"
)

const schemaVersion = "mac-cleanup-studio/v1"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	output := &countWriter{Writer: stdout}
	stdout = output
	opts, err := cli.Parse(args)
	if err != nil {
		for _, arg := range args {
			if arg == "--json" || arg == "-json" || arg == "--json=true" || arg == "-json=true" {
				writeCommandError(stdout, "invalid_arguments")
				break
			}
		}
		fmt.Fprintf(stderr, "Error: %v\n\n%s", err, cli.Usage())
		return 2
	}
	if opts.Command == cli.CommandHelp {
		fmt.Fprint(stdout, cli.Usage())
		return 0
	}
	if opts.Command == cli.CommandVersion {
		fmt.Fprintf(stdout, "mac-cleanup-studio %s (commit %s, built %s)\n", version, commit, buildDate)
		return 0
	}
	if opts.Command == cli.CommandDiagnostics {
		if err := runDiagnostics(stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if runtime.GOOS != "darwin" {
		if opts.JSON {
			writeCommandError(stdout, "unsupported_platform")
		}
		fmt.Fprintln(stderr, "Error: Mac Cleanup Studio currently supports macOS only.")
		return 1
	}

	home, err := os.UserHomeDir()
	if err != nil {
		if opts.JSON {
			writeCommandError(stdout, "operation_failed")
		}
		fmt.Fprintf(stderr, "Error: find current user home: %v\n", err)
		return 1
	}
	engine, err := cleanup.NewDefault(home)
	if err != nil {
		if opts.JSON {
			writeCommandError(stdout, cleanup.ErrorCode(err))
		}
		fmt.Fprintf(stderr, "Error: initialize cleanup engine: %v\n", err)
		return 1
	}

	switch opts.Command {
	case cli.CommandDashboard:
		err = runDashboard(ctx, opts, engine, home, stdout, stderr)
	case cli.CommandCapabilities:
		err = runCapabilities(opts, engine, stdout)
	case cli.CommandScan:
		err = runScan(ctx, opts, engine, home, stdout)
	case cli.CommandExplore:
		err = runExplore(ctx, opts, engine, stdout)
	case cli.CommandRecommend:
		err = runRecommend(ctx, opts, engine, home, stdout)
	case cli.CommandClean, cli.CommandAuto:
		err = runCleanup(ctx, opts, engine, home, stdout)
	default:
		err = fmt.Errorf("unsupported command %q", opts.Command)
	}
	if err == nil {
		return 0
	}
	if opts.JSON && output.n == 0 {
		writeCommandError(stdout, cleanup.ErrorCode(err))
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "Cancelled.")
		return 130
	}
	fmt.Fprintf(stderr, "Error: %v\n", err)
	return 1
}

func runDashboard(ctx context.Context, opts cli.Options, engine *cleanup.Engine, home string, stdout, stderr io.Writer) error {
	service, err := app.NewDashboardService(engine, home)
	if err != nil {
		return err
	}
	token, err := sessionToken()
	if err != nil {
		return err
	}
	server, err := dashboard.NewServer(opts.Listen, dashboard.Config{Token: token, Service: service, Context: ctx})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", opts.Listen)
	if err != nil {
		return fmt.Errorf("start local dashboard listener: %w", err)
	}
	defer listener.Close()

	launchURL := dashboardURL(listener.Addr(), token)
	fmt.Fprintf(stdout, "Mac Cleanup Studio is running locally.\nDashboard: %s\nPress Ctrl+C to stop.\n", launchURL)
	if !opts.NoOpen {
		if err := exec.Command("open", launchURL).Start(); err != nil {
			fmt.Fprintf(stderr, "Could not open the browser automatically: %v\n", err)
		}
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("stop local dashboard: %w", err)
		}
		err := <-serveErr
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func runScan(ctx context.Context, opts cli.Options, engine *cleanup.Engine, home string, output io.Writer) error {
	ruleIDs, profile, err := scanSelection(opts, engine)
	if err != nil {
		return err
	}
	volume, err := cleanup.VolumeStats(home)
	if err != nil {
		return fmt.Errorf("read disk usage: %w", err)
	}
	scan, err := engine.Scan(ctx, cleanup.ScanOptions{RuleIDs: ruleIDs})
	if err != nil {
		return err
	}
	if opts.JSON {
		return writeJSON(output, struct {
			SchemaVersion string             `json:"schema_version"`
			Mode          string             `json:"mode"`
			Profile       string             `json:"profile"`
			RuleIDs       []string           `json:"rule_ids"`
			Volume        cleanup.VolumeInfo `json:"volume"`
			Scan          *cleanup.Scan      `json:"scan"`
		}{SchemaVersion: schemaVersion, Mode: "scan", Profile: profile, RuleIDs: ruleIDs, Volume: volume, Scan: scan})
	}
	printScan(output, scan, volume, "Read-only scan", false)
	return nil
}

func scanSelection(opts cli.Options, engine *cleanup.Engine) ([]string, string, error) {
	if len(opts.RuleIDs) > 0 {
		rules, err := app.ValidateScanRuleIDs(engine.Rules(), opts.RuleIDs)
		return rules, "explicit", err
	}
	rules, err := app.RuleIDs(engine.Rules(), opts.Profile, false)
	return rules, opts.Profile, err
}

type commandCapability struct {
	Name        string `json:"name"`
	ReadOnly    bool   `json:"read_only"`
	JSON        bool   `json:"json"`
	Description string `json:"description"`
}

type profileCapability struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

func runCapabilities(opts cli.Options, engine *cleanup.Engine, output io.Writer) error {
	commands := []commandCapability{
		{Name: cli.CommandDiagnostics, ReadOnly: true, JSON: true, Description: "Explicit --share exports an allowlisted diagnostic summary; no scan, paths or upload."},
		{Name: cli.CommandCapabilities, ReadOnly: true, JSON: true, Description: "Discover the versioned CLI contract and compiled cleanup rules."},
		{Name: cli.CommandScan, ReadOnly: true, JSON: true, Description: "Measure selected categories and list candidates."},
		{Name: cli.CommandExplore, ReadOnly: true, JSON: true, Description: "Inspect folder sizes and large/old files in fixed personal scopes; never deletes."},
		{Name: cli.CommandRecommend, ReadOnly: true, JSON: true, Description: "Explain deterministic cleanup recommendations."},
		{Name: cli.CommandClean, ReadOnly: false, JSON: true, Description: "Preview cleanup; --interactive reviews individual candidates. Apply requires --apply --yes; interactive apply also requires DELETE."},
		{Name: cli.CommandAuto, ReadOnly: false, JSON: true, Description: "Use the fixed safe profile; still requires --apply --yes to delete."},
		{Name: cli.CommandDashboard, ReadOnly: false, JSON: false, Description: "Run the optional local interactive client."},
	}
	profiles := []profileCapability{
		{ID: cli.ProfileSafe, Description: "Allowlisted low-risk rules marked for automatic selection."},
		{ID: cli.ProfileBalanced, Description: "All cleanup-capable rules, including caution rules."},
		{ID: cli.ProfileReview, Description: "All rules for inspection; report-only rules remain non-destructive."},
		{ID: cli.ProfileAll, Description: "All rules for inspection; cleanup excludes report-only rules."},
	}
	document := struct {
		SchemaVersion     string                 `json:"schema_version"`
		Product           string                 `json:"product"`
		Version           string                 `json:"version"`
		Commit            string                 `json:"commit"`
		BuildDate         string                 `json:"build_date"`
		BuildIdentity     string                 `json:"build_identity"`
		Commands          []commandCapability    `json:"commands"`
		Profiles          []profileCapability    `json:"profiles"`
		Rules             []cleanup.RuleInfo     `json:"rules"`
		ExplorationScopes []cleanup.ExploreScope `json:"exploration_scopes"`
		Safety            map[string]any         `json:"safety"`
		ExitCodes         map[string]int         `json:"exit_codes"`
	}{
		SchemaVersion:     schemaVersion,
		Product:           "mac-cleanup-studio",
		Version:           version,
		Commit:            commit,
		BuildDate:         buildDate,
		BuildIdentity:     buildIdentity,
		Commands:          commands,
		Profiles:          profiles,
		Rules:             engine.Rules(),
		ExplorationScopes: cleanup.ExplorationScopes(),
		Safety: map[string]any{
			"destructive_by_default":    false,
			"arbitrary_paths_accepted":  false,
			"requires_sudo":             false,
			"network_required":          false,
			"cli_confirmation":          []string{"--apply", "--yes"},
			"dashboard_confirmation":    "DELETE",
			"interactive_confirmation":  "DELETE",
			"scan_bound_candidate_ids":  true,
			"single_use_cleanup_plans":  true,
			"exclusions_config":         "~/" + cleanup.ProtectionConfig,
			"scan_max_entries":          200000,
			"scan_max_candidates":       2000,
			"scan_max_warnings":         100,
			"scan_max_depth":            64,
			"storage_map_max_nodes":     cleanup.StorageMapLimit,
			"operation_timeout_seconds": 120,
		},
		ExitCodes: map[string]int{"success": 0, "runtime_or_partial_failure": 1, "usage": 2, "cancelled": 130},
	}
	if opts.JSON {
		return writeJSON(output, document)
	}
	fmt.Fprintf(output, "Mac Cleanup Studio %s — agent-agnostic CLI contract %s\n\n", version, schemaVersion)
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "COMMAND\tREAD ONLY\tJSON\tPURPOSE")
	for _, command := range commands {
		fmt.Fprintf(table, "%s\t%t\t%t\t%s\n", command.Name, command.ReadOnly, command.JSON, command.Description)
	}
	_ = table.Flush()
	fmt.Fprintln(output, "\nNo command accepts arbitrary paths. Deletion requires both --apply and --yes.")
	fmt.Fprintln(output, "Use `capabilities --json` to discover profiles, rules, safety flags, and exit codes.")
	return nil
}

func runRecommend(ctx context.Context, opts cli.Options, engine *cleanup.Engine, home string, output io.Writer) error {
	ruleIDs, profile, err := scanSelection(opts, engine)
	if err != nil {
		return err
	}
	volume, err := cleanup.VolumeStats(home)
	if err != nil {
		return fmt.Errorf("read disk usage: %w", err)
	}
	scan, err := engine.Scan(ctx, cleanup.ScanOptions{RuleIDs: ruleIDs})
	if err != nil {
		return err
	}
	recommendations := app.Recommendations(scan)
	suggested := suggestedRuleIDs(recommendations)
	if opts.JSON {
		return writeJSON(output, struct {
			SchemaVersion    string               `json:"schema_version"`
			Mode             string               `json:"mode"`
			Profile          string               `json:"profile"`
			RuleIDs          []string             `json:"rule_ids"`
			ScanID           string               `json:"scan_id"`
			StartedAt        time.Time            `json:"started_at"`
			CompletedAt      time.Time            `json:"completed_at"`
			Volume           cleanup.VolumeInfo   `json:"volume"`
			Recommendations  []app.Recommendation `json:"recommendations"`
			SuggestedRuleIDs []string             `json:"suggested_rule_ids"`
			Warnings         []cleanup.Warning    `json:"warnings,omitempty"`
			Partial          bool                 `json:"partial"`
			WarningsOmitted  int                  `json:"warnings_omitted"`
		}{
			SchemaVersion: schemaVersion, Mode: "recommend", Profile: profile, RuleIDs: ruleIDs,
			ScanID: scan.ID, StartedAt: scan.StartedAt, CompletedAt: scan.CompletedAt, Volume: volume,
			Recommendations: recommendations, SuggestedRuleIDs: suggested, Warnings: scan.Warnings,
			Partial: scan.Partial, WarningsOmitted: scan.WarningsOmitted,
		})
	}
	fmt.Fprintf(output, "Mac Cleanup Studio — deterministic recommendations\nAvailable: %s of %s | Scan: %s\n\n",
		cleanup.FormatBytes(volume.AvailableBytes), cleanup.FormatBytes(volume.TotalBytes), scan.ID)
	if scan.Partial {
		fmt.Fprintf(output, "Incomplete coverage: %d warnings, %d omitted. Recommendations cover measured items only.\n", len(scan.Warnings), scan.WarningsOmitted)
	}
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "CATEGORY\tRISK\tDECISION\tRECLAIMABLE\tDETECTED\tWHY")
	for _, recommendation := range recommendations {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n", recommendation.Name, recommendation.Risk,
			recommendation.Decision, cleanup.FormatBytes(recommendation.Reclaimable.AllocatedBytes),
			cleanup.FormatBytes(recommendation.Detected.AllocatedBytes), recommendation.Reason)
	}
	_ = table.Flush()
	if len(suggested) > 0 {
		fmt.Fprintf(output, "\nSuggested dry run: mac-cleanup-studio clean --rules %s\n", strings.Join(suggested, ","))
	} else {
		fmt.Fprintln(output, "\nNo low-risk rules currently qualify for the suggested dry run.")
	}
	fmt.Fprintln(output, "Recommendations never delete files. Any applied cleanup still requires --apply --yes.")
	return nil
}

func suggestedRuleIDs(recommendations []app.Recommendation) []string {
	ids := make([]string, 0)
	for _, recommendation := range recommendations {
		if recommendation.Decision == app.DecisionAutoClean {
			ids = append(ids, recommendation.RuleID)
		}
	}
	return ids
}

func runCleanup(ctx context.Context, opts cli.Options, engine *cleanup.Engine, home string, output io.Writer) error {
	return runCleanupWithInput(ctx, opts, engine, home, output, os.Stdin, os.Stderr)
}

func runCleanupWithInput(ctx context.Context, opts cli.Options, engine *cleanup.Engine, home string, output io.Writer, input io.Reader, review io.Writer) error {
	ruleIDs := opts.RuleIDs
	profile := opts.Profile
	var err error
	if len(ruleIDs) > 0 {
		profile = "explicit"
		ruleIDs, err = app.ValidateRuleIDs(engine.Rules(), ruleIDs)
	} else {
		ruleIDs, err = app.RuleIDs(engine.Rules(), opts.Profile, true)
	}
	if err != nil {
		return err
	}
	before, err := cleanup.VolumeStats(home)
	if err != nil {
		return fmt.Errorf("read disk usage: %w", err)
	}
	scan, err := engine.Scan(ctx, cleanup.ScanOptions{RuleIDs: ruleIDs})
	if err != nil {
		return err
	}

	cleanOptions := cleanup.CleanOptions{RuleIDs: ruleIDs}
	if opts.Interactive {
		ids, err := reviewCandidates(ctx, input, review, engine, scan, opts.Apply)
		if err != nil {
			return err
		}
		cleanOptions = cleanup.CleanOptions{CandidateIDs: ids}
	}
	selection, err := engine.PreviewSelection(scan, cleanOptions)
	if err != nil {
		return err
	}
	mode := "dry_run"
	var result *cleanup.CleanResult
	var cleanErr error
	after := before
	observationAvailable := false
	if opts.Apply {
		mode = "applied"
		if !opts.JSON && !opts.Interactive {
			printScan(output, scan, before, "Cleanup plan authorized for this run", true)
			fmt.Fprintln(output, "\nPermanent deletion is starting because --apply --yes was supplied.")
		}
		result, cleanErr = engine.Clean(ctx, scan, cleanOptions)
		if errors.Is(cleanErr, context.Canceled) {
			mode = "interrupted"
		} else if cleanErr != nil {
			mode = "failed"
		}
		measuredAfter, measureErr := cleanup.VolumeStats(home)
		if measureErr != nil {
			if cleanErr == nil {
				cleanErr = fmt.Errorf("read disk usage after cleanup: %w", measureErr)
				mode = "failed"
			}
		} else {
			after = measuredAfter
			observationAvailable = true
		}
	}
	if cleanErr == nil && result != nil && (len(result.Failures) > 0 || len(result.Rejected) > 0) {
		cleanErr = cleanup.ErrPartial
		mode = "partial"
	}

	observed := observedIncrease(after.AvailableBytes, before.AvailableBytes)
	if opts.JSON {
		errorMessage := ""
		if cleanErr != nil {
			errorMessage = cleanErr.Error()
		}
		if err := writeJSON(output, struct {
			SchemaVersion        string               `json:"schema_version"`
			Mode                 string               `json:"mode"`
			Profile              string               `json:"profile"`
			RuleIDs              []string             `json:"rule_ids"`
			Scan                 *cleanup.Scan        `json:"scan"`
			Selection            *cleanup.Selection   `json:"selection"`
			Result               *cleanup.CleanResult `json:"result,omitempty"`
			VolumeBefore         cleanup.VolumeInfo   `json:"volume_before"`
			VolumeAfter          cleanup.VolumeInfo   `json:"volume_after"`
			ObservedFreeChange   int64                `json:"observed_free_space_change_bytes"`
			Error                string               `json:"error,omitempty"`
			ErrorCode            string               `json:"error_code,omitempty"`
			ObservationAvailable bool                 `json:"observation_available"`
		}{
			SchemaVersion: schemaVersion, Mode: mode, Profile: profile, RuleIDs: ruleIDs, Scan: scan, Selection: selection, Result: result,
			VolumeBefore: before, VolumeAfter: after, ObservedFreeChange: observed, Error: errorMessage,
			ErrorCode: cleanup.ErrorCode(cleanErr), ObservationAvailable: observationAvailable,
		}); err != nil {
			return err
		}
	} else {
		title := "Cleanup dry run"
		if opts.Command == cli.CommandAuto {
			title = "Safe auto-clean dry run"
		}
		if !opts.Apply {
			if opts.Interactive {
				fmt.Fprintf(output, "Selected %d candidates: %s allocated (%s logical).\n", len(selection.CandidateIDs), cleanup.FormatBytes(selection.Metrics.AllocatedBytes), cleanup.FormatBytes(selection.Metrics.LogicalBytes))
			} else {
				printScan(output, scan, before, title, true)
			}
			fmt.Fprintln(output, "\nNo files were deleted. Rerun with --apply --yes to create, revalidate, and apply a fresh plan.")
		} else {
			printCleanResult(output, result, observed, observationAvailable)
			if cleanErr != nil {
				fmt.Fprintf(output, "Cleanup stopped: %v. The result above may be partial.\n", cleanErr)
			}
		}
	}

	if cleanErr != nil {
		return cleanErr
	}
	return nil
}

func printScan(output io.Writer, scan *cleanup.Scan, volume cleanup.VolumeInfo, title string, showPlan bool) {
	fmt.Fprintf(output, "Mac Cleanup Studio — %s\n", title)
	if scan.Partial {
		fmt.Fprintf(output, "Incomplete coverage: totals cover measured items only; %d additional warnings omitted.\n", scan.WarningsOmitted)
	}
	fmt.Fprintf(output, "Available: %s of %s | Scan: %s\n\n",
		cleanup.FormatBytes(volume.AvailableBytes), cleanup.FormatBytes(volume.TotalBytes), scan.ID)

	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "CATEGORY\tRISK\tELIGIBLE\tEST. RECLAIM\tDETECTED\tACTION")
	for _, rule := range scan.Rules {
		eligible := 0
		for _, candidate := range rule.Candidates {
			if candidate.Eligible {
				eligible++
			}
		}
		count := fmt.Sprintf("%d/%d", eligible, len(rule.Candidates))
		action := "clean"
		if rule.Rule.Action == cleanup.ActionScanOnly {
			action = "review only"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n",
			rule.Rule.Name, rule.Rule.Risk, count,
			cleanup.FormatBytes(rule.Eligible.AllocatedBytes), cleanup.FormatBytes(rule.Detected.AllocatedBytes), action)
	}
	_ = table.Flush()
	if showPlan {
		fmt.Fprintln(output, "\nEligible candidates in this plan:")
		plan := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
		fmt.Fprintln(plan, "RULE\tEST. RECLAIM\tPATH")
		count := 0
		for _, rule := range scan.Rules {
			for _, candidate := range rule.Candidates {
				if !candidate.Eligible {
					continue
				}
				count++
				fmt.Fprintf(plan, "%s\t%s\t%s\n", rule.Rule.ID,
					cleanup.FormatBytes(candidate.Metrics.AllocatedBytes), candidate.DisplayPath)
			}
		}
		if count == 0 {
			fmt.Fprintln(plan, "—\t0 B\tNo candidates pass the current age rules")
		}
		_ = plan.Flush()
	}
	fmt.Fprintf(output, "\nEstimated reclaimable: %s allocated (%s logical)\n",
		cleanup.FormatBytes(scan.Eligible.AllocatedBytes), cleanup.FormatBytes(scan.Eligible.LogicalBytes))
	if len(scan.Warnings) > 0 {
		fmt.Fprintf(output, "Scan warnings: %d\n", len(scan.Warnings))
		for _, warning := range scan.Warnings {
			fmt.Fprintf(output, "  - %s\n", warning.Message)
		}
	}
	fmt.Fprintln(output, "APFS snapshots, clones, compression, and concurrent writes can change the observed free-space result.")
}

func printCleanResult(output io.Writer, result *cleanup.CleanResult, observed int64, observationAvailable bool) {
	if result == nil {
		return
	}
	fmt.Fprintf(output, "\nRemoved candidates: %d\n", len(result.Removed))
	fmt.Fprintf(output, "Removed estimate: %s allocated (%s logical)\n",
		cleanup.FormatBytes(result.Freed.AllocatedBytes), cleanup.FormatBytes(result.Freed.LogicalBytes))
	if !observationAvailable {
		fmt.Fprintln(output, "Free-space observation unavailable.")
	} else if observed >= 0 {
		fmt.Fprintf(output, "Observed free-space increase: %s\n", cleanup.FormatBytes(observed))
	} else {
		fmt.Fprintf(output, "Observed free-space change: %s\n", cleanup.FormatBytes(observed))
	}
	if count := len(result.Failures) + len(result.Rejected); count > 0 {
		fmt.Fprintf(output, "Not removed: %d candidates\n", count)
	}
}

func sessionToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("create dashboard session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func dashboardURL(addr net.Addr, token string) string {
	host := addr.String()
	if tcp, ok := addr.(*net.TCPAddr); ok {
		host = net.JoinHostPort(tcp.IP.String(), fmt.Sprintf("%d", tcp.Port))
	}
	return (&url.URL{Scheme: "http", Host: host, Path: "/", Fragment: "token=" + token}).String()
}

func observedIncrease(after, before int64) int64 {
	return after - before
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
