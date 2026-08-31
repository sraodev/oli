package main

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/sraodev/mac-cleanup-studio/internal/cleanup"
	"github.com/sraodev/mac-cleanup-studio/internal/cli"
)

func runExplore(ctx context.Context, opts cli.Options, engine *cleanup.Engine, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	report, err := engine.Explore(ctx, cleanup.ExploreOptions{
		Scope: opts.Scope, MinSizeBytes: opts.MinSizeMiB << 20,
		OlderThanDays: opts.OlderThanDays, Limit: opts.Limit,
	})
	if err != nil {
		return err
	}
	if opts.JSON {
		return writeJSON(output, struct {
			SchemaVersion string               `json:"schema_version"`
			Mode          string               `json:"mode"`
			Report        *cleanup.Exploration `json:"report"`
		}{SchemaVersion: schemaVersion, Mode: "explore", Report: report})
	}
	fmt.Fprintf(output, "Storage Atlas — read-only · %s\n", report.Scope)
	fmt.Fprintf(output, "%s allocated · %s logical · %d unique files\n",
		cleanup.FormatBytes(report.Total.AllocatedBytes), cleanup.FormatBytes(report.Total.LogicalBytes), report.Total.UniqueFiles)
	for _, scope := range report.Scopes {
		fmt.Fprintf(output, "%s: %s allocated\n", scope.DisplayPath, cleanup.FormatBytes(scope.Metrics.AllocatedBytes))
	}
	for _, section := range []struct {
		title string
		items []cleanup.ExploreItem
	}{
		{"Largest top-level folders", report.Folders},
		{"Large files", report.LargeFiles},
		{"Old files (by modification date, not last use)", report.OldFiles},
	} {
		fmt.Fprintf(output, "\n%s (up to %d)\n", section.title, report.Limit)
		table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
		fmt.Fprintln(table, "ALLOCATED\tLOGICAL\tMODIFIED\tPATH")
		for _, item := range section.items {
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", cleanup.FormatBytes(item.Metrics.AllocatedBytes), cleanup.FormatBytes(item.Metrics.LogicalBytes), item.Modified.Format("2006-01-02"), item.DisplayPath)
		}
		_ = table.Flush()
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(output, "Warning: %s: %s\n", warning.DisplayPath, warning.Message)
	}
	if report.Partial {
		fmt.Fprintln(output, "This report is partial. Unreadable or bounded areas are not included in totals.")
	}
	fmt.Fprintln(output, "These are size estimates, not reclaimable space. Personal files are never selected for deletion.")
	return nil
}
