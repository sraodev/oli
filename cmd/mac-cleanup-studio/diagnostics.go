package main

import (
	"fmt"
	"io"
	"runtime"
)

// Use an allowlist instead of attempting to redact arbitrary logs or filenames.
// This command does not open the home directory, read environment values or scan.
func runDiagnostics(output io.Writer) error {
	return writeJSON(output, struct {
		SchemaVersion string `json:"schema_version"`
		Mode          string `json:"mode"`
		Product       string `json:"product"`
		Platform      string `json:"platform"`
		Architecture  string `json:"architecture"`
		GoVersion     string `json:"go_version"`
		DataPolicy    string `json:"data_policy"`
	}{schemaVersion, "diagnostics", "mac-cleanup-studio", runtime.GOOS, runtime.GOARCH, runtime.Version(), "No paths, filenames, usernames, environment, tokens, scan data or logs collected. Nothing uploaded."})
}

type countWriter struct {
	io.Writer
	n int
}

func (w *countWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.n += n
	return n, err
}

func writeCommandError(output io.Writer, code string) {
	_ = writeJSON(output, struct {
		SchemaVersion string `json:"schema_version"`
		Mode          string `json:"mode"`
		Code          string `json:"code"`
		Error         string `json:"error"`
	}{schemaVersion, "error", code, fmt.Sprintf("Command did not complete (%s). Local details are on stderr.", code)})
}
