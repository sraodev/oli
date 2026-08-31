package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRejectReleaseArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"--version", "../bad"}, {"--version", "v1.2.3"}, {"--version", "v1.2.3", "--out", "x", "--verify", "x"}} {
		if run(args) == nil {
			t.Fatalf("accepted %q", args)
		}
	}
}

// Real compiler, archives, installer and native executable. Every mutation is
// confined to t.TempDir; no cleanup command is run against the user's home.
func TestReleaseRehearsalAndInstallerE2E(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("installer requires macOS")
	}
	t.Chdir("../..")
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, dir := range []string{a, b} {
		if err := build(dir, "v0.0.0", true); err != nil {
			t.Fatal(err)
		}
		if err := verifyDirectory(dir, "v0.0.0", true); err != nil {
			t.Fatal(err)
		}
		if err := verifyDirectory(dir, "v0.0.0", false); err == nil {
			t.Fatal("snapshot accepted as release")
		}
	}
	entries, err := os.ReadDir(a)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		left, err := os.ReadFile(filepath.Join(a, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		right, err := os.ReadFile(filepath.Join(b, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(left, right) {
			t.Fatalf("non-reproducible %s", entry.Name())
		}
	}
	if err := build(a, "v0.0.0", true); err == nil {
		t.Fatal("overwrote artifact directory")
	}
	archiveName := bundle("v0.0.0", runtime.GOARCH) + ".tar.gz"
	archive := filepath.Join(a, archiveName)
	sums := filepath.Join(a, "SHA256SUMS")
	bin := filepath.Join(base, "bin")
	install := func(success bool, args ...string) {
		t.Helper()
		cmd := exec.Command("bash", append([]string{"scripts/install.sh"}, args...)...)
		out, err := cmd.CombinedOutput()
		if (err == nil) != success {
			t.Fatalf("installer %q: %v\n%s", args, err, out)
		}
	}
	install(true, "install", "v0.0.0", archive, sums, bin)
	target := filepath.Join(bin, product)
	original, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	archiveBytes, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := verifyArchive(archiveBytes, "v0.0.0", runtime.GOARCH, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"version", "commit", "architecture", "signing"} {
		changed := meta
		switch field {
		case "version":
			changed.Version = "v0.0.1"
		case "commit":
			changed.Commit = strings.Repeat("b", 40)
		case "architecture":
			if runtime.GOARCH == "arm64" {
				changed.Architecture = "amd64"
			} else {
				changed.Architecture = "arm64"
			}
		case "signing":
			changed.Signing = "unsupported signing claim"
		}
		mismatch, err := pack(original, []byte("license"), changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := verifyArchive(mismatch, changed.Version, changed.Architecture, true); err == nil {
			t.Fatalf("accepted mismatched %s", field)
		}
	}
	if out, err := command(context.Background(), target, "version"); err != nil || !strings.HasPrefix(out, product+" v0.0.0 (commit ") {
		t.Fatalf("version: %s %v", out, err)
	}
	install(false, "install", "v0.0.0", archive, sums, bin)
	install(false, "install", "v0.0.1", archive, sums, bin, "--replace")
	install(true, "install", "v0.0.0", archive, sums, bin, "--replace")
	if err := os.WriteFile(filepath.Join(bin, "unrelated"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	badArchive := filepath.Join(base, "bad.tar.gz")
	if err := os.WriteFile(badArchive, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	install(false, "install", "v0.0.0", badArchive, sums, bin, "--replace")
	// Even a matching checksum cannot make an invalid archive installable.
	badSums := filepath.Join(base, "bad-sums")
	if err := os.WriteFile(badSums, []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte("corrupt")), archiveName)), 0600); err != nil {
		t.Fatal(err)
	}
	install(false, "install", "v0.0.0", badArchive, badSums, bin, "--replace")
	current, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(original, current) {
		t.Fatal("failed update damaged existing installation")
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(bin, alias); err != nil {
		t.Fatal(err)
	}
	install(false, "install", "v0.0.0", archive, sums, alias, "--replace")
	install(false, "uninstall", bin)
	install(true, "uninstall", bin, "--yes")
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("uninstall retained binary")
	}
	if data, err := os.ReadFile(filepath.Join(bin, "unrelated")); err != nil || string(data) != "keep" {
		t.Fatal("uninstall touched unrelated file")
	}
	// A tampered download fails verification, before any installer execution.
	if err := os.WriteFile(filepath.Join(b, archiveName), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if verifyDirectory(b, "v0.0.0", true) == nil {
		t.Fatal("tampered download accepted")
	}
}

func TestArchiveRejectsMalformedBytes(t *testing.T) {
	meta := provenance{"v0.0.0", strings.Repeat("a", 40), "2026-09-01T00:00:00Z", runtime.Version(), "arm64", true, "No Developer ID signature; not notarized"}
	data, err := pack([]byte("not Mach-O"), []byte("license"), meta)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{data, data[:len(data)-1], []byte("invalid")} {
		if _, err := verifyArchive(data, "v0.0.0", "arm64", true); err == nil {
			t.Fatal("invalid archive accepted")
		}
	}
}

func TestPublicationStopsBeforeVisibility(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	if strings.Contains(workflow, "\n  push:") || !strings.Contains(workflow, "prevent_self_review == true") || !strings.Contains(workflow, "environment: release") {
		t.Fatal("release approval gate missing")
	}
	marker := "          # create fails if a release already exists; never replace existing bytes.\n"
	_, script, ok := strings.Cut(workflow, marker)
	if !ok {
		t.Fatal("publication script missing")
	}
	for _, failAt := range []string{"create", "download", "verify", "none"} {
		t.Run(failAt, func(t *testing.T) {
			base := t.TempDir()
			bin := filepath.Join(base, "mock-bin")
			assets := filepath.Join(base, "dist", "release-a")
			for _, dir := range []string{bin, assets} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"a.tar.gz", "SHA256SUMS", "install.sh"} {
				if err := os.WriteFile(filepath.Join(assets, name), []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			gh := `#!/bin/bash
set -eu
echo "$2" >> "$MCS_TEST_LOG"
[[ "$MCS_TEST_FAIL" != "$2" ]] || exit 1
if [[ "$2" == download ]]; then
  mkdir -p dist/downloaded
  cp dist/release-a/* dist/downloaded/
fi
`
			goStub := `#!/bin/bash
[[ "$MCS_TEST_FAIL" != verify ]]
`
			for name, body := range map[string]string{"gh": gh, "go": goStub} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0700); err != nil {
					t.Fatal(err)
				}
			}
			log := filepath.Join(base, "log")
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", script)
			cmd.Dir = base
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "MCS_TEST_FAIL="+failAt, "MCS_TEST_LOG="+log, "VERSION=v0.0.0", "REPO=fixture/local")
			out, err := cmd.CombinedOutput()
			if (err == nil) != (failAt == "none") {
				t.Fatalf("%s: %v %s", failAt, err, out)
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(calls), "edit") != (failAt == "none") {
				t.Fatalf("premature publication: %s", calls)
			}
		})
	}
}
