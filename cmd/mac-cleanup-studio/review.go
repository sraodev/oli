package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/sraodev/mac-cleanup-studio/internal/cleanup"
)

// The menu and its IDs live in the same process as the private scan. Row numbers
// are never accepted from a previous scan or converted into filesystem paths.
func reviewCandidates(ctx context.Context, input io.Reader, output io.Writer, engine *cleanup.Engine, scan *cleanup.Scan, apply bool) ([]string, error) {
	var candidates []cleanup.Candidate
	fmt.Fprintln(output, "Review this scan only. A directory candidate includes its scanned contents.")
	for _, rule := range scan.Rules {
		for _, candidate := range rule.Candidates {
			label := "not eligible"
			if candidate.Eligible {
				candidates = append(candidates, candidate)
				label = strconv.Itoa(len(candidates))
			}
			fmt.Fprintf(output, "%s | %s | %s allocated | %s logical | modified %s | %s risk | %s\n", label, candidate.DisplayPath,
				cleanup.FormatBytes(candidate.Metrics.AllocatedBytes), cleanup.FormatBytes(candidate.Metrics.LogicalBytes), candidate.LatestModified.Format("2006-01-02"), rule.Rule.Risk, candidate.EligibilityReason)
		}
	}
	for _, warning := range scan.Warnings {
		fmt.Fprintf(output, "Warning: %s\n", warning.Message)
	}
	if len(candidates) == 0 {
		return nil, errors.New("no eligible candidates to review")
	}
	fmt.Fprintln(output, "Enter comma-separated row numbers to keep selected (empty cancels):")
	scanner := bufio.NewScanner(input)
	line, err := reviewLine(ctx, scanner)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(line) == "" {
		return nil, context.Canceled
	}
	var ids []string
	for _, value := range strings.Split(line, ",") {
		row, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || row < 1 || row > len(candidates) {
			return nil, errors.New("selection must contain current candidate row numbers only")
		}
		ids = append(ids, candidates[row-1].ID)
	}
	selection, err := engine.PreviewSelection(scan, cleanup.CleanOptions{CandidateIDs: ids})
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(output, "Selected %d candidates: %s allocated (%s logical). Estimates are not guaranteed reclaimed space.\n", len(ids), cleanup.FormatBytes(selection.Metrics.AllocatedBytes), cleanup.FormatBytes(selection.Metrics.LogicalBytes))
	if apply {
		fmt.Fprintln(output, "Type DELETE to permanently remove only these candidates:")
		confirmation, err := reviewLine(ctx, scanner)
		if err != nil {
			return nil, err
		}
		if confirmation != "DELETE" {
			return nil, errors.New("confirmation must be exactly DELETE")
		}
	}
	return ids, nil
}

func reviewLine(ctx context.Context, scanner *bufio.Scanner) (string, error) {
	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		if scanner.Scan() {
			done <- result{text: scanner.Text()}
			return
		}
		err := scanner.Err()
		if err == nil {
			err = io.EOF
		}
		done <- result{err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case value := <-done:
		return value.text, value.err
	}
}
