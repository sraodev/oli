// Command inspect demonstrates the read-only Oli JSON integration contract.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

const supportedSchema = "mac-cleanup-studio/v1"

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: inspect /path/to/oli")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := inspect(ctx, os.Args[1], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		os.Exit(1)
	}
}

func inspect(ctx context.Context, binary string, output io.Writer) error {
	var capabilities struct {
		SchemaVersion string `json:"schema_version"`
		Commands      []struct {
			Name     string `json:"name"`
			ReadOnly bool   `json:"read_only"`
			JSON     bool   `json:"json"`
		} `json:"commands"`
		Scopes []struct {
			ID string `json:"id"`
		} `json:"exploration_scopes"`
	}
	if err := commandJSON(ctx, binary, &capabilities, "capabilities", "--json"); err != nil {
		return err
	}
	if capabilities.SchemaVersion != supportedSchema {
		return errors.New("unsupported Oli schema; no inspection attempted")
	}
	canExplore, hasDownloads := false, false
	for _, command := range capabilities.Commands {
		if command.Name == "explore" && command.ReadOnly && command.JSON {
			canExplore = true
		}
	}
	for _, scope := range capabilities.Scopes {
		if scope.ID == "downloads" {
			hasDownloads = true
		}
	}
	if !canExplore || !hasDownloads {
		return errors.New("read-only JSON downloads exploration is unavailable")
	}
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		Mode          string `json:"mode"`
		Report        *struct {
			Action  string `json:"action"`
			Scope   string `json:"scope"`
			Partial *bool  `json:"partial"`
			Total   struct {
				LogicalBytes *int64 `json:"logical_bytes"`
			} `json:"total"`
			Warnings []struct {
				Code string `json:"code"`
			} `json:"warnings"`
		} `json:"report"`
	}
	if err := commandJSON(ctx, binary, &envelope, "explore", "--scope", "downloads", "--limit", "10", "--json"); err != nil {
		return err
	}
	r := envelope.Report
	if envelope.SchemaVersion != supportedSchema || envelope.Mode != "explore" || r == nil ||
		r.Action != "scan_only" || r.Scope != "downloads" || r.Partial == nil ||
		r.Total.LogicalBytes == nil || *r.Total.LogicalBytes < 0 {
		return errors.New("invalid or unsupported exploration report")
	}
	// This is an inspection summary, never an estimate of space freed. Omit
	// paths and warning messages, which may contain private local filenames.
	return json.NewEncoder(output).Encode(struct {
		Scope        string `json:"scope"`
		Partial      bool   `json:"partial"`
		LogicalBytes int64  `json:"logical_bytes"`
		WarningCount int    `json:"warning_count"`
	}{r.Scope, *r.Partial || len(r.Warnings) > 0, *r.Total.LogicalBytes, len(r.Warnings)})
}

func commandJSON(ctx context.Context, binary string, document any, args ...string) error {
	command := exec.CommandContext(ctx, binary, args...)
	command.WaitDelay = time.Second
	data, err := command.Output()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if exit.ExitCode() == 130 {
				return context.Canceled
			}
			return fmt.Errorf("Oli %s failed (exit %d); no report accepted", args[0], exit.ExitCode())
		}
		return fmt.Errorf("Oli %s could not complete; no report accepted", args[0])
	}
	if err := json.Unmarshal(data, document); err != nil {
		return fmt.Errorf("Oli %s returned invalid JSON", args[0])
	}
	return nil
}
