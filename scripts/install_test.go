package scripts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// These tests run the real Bash installer and a real Go binary, but use only
// synthetic release archives and an isolated installation under the test root.
func TestInstallerE2E(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native macOS installer acceptance requires Darwin")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	// /tmp has a world-writable ancestor and is intentionally not an install root.
	base, err := os.MkdirTemp(home, ".oli-installer-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	binary := filepath.Join(base, "release-binary")
	arch := runtime.GOARCH
	if out, _ := exec.Command("/usr/sbin/sysctl", "-in", "sysctl.proc_translated").Output(); strings.TrimSpace(string(out)) == "1" {
		arch = "arm64"
	}
	build := func(version, buildArch string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags", "-X main.version="+version+" -X main.commit=fixture -X main.buildDate=fixture", "-o", binary, "../cmd/oli")
		cmd.Env = append(os.Environ(), "GOARCH="+buildArch, "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, out)
		}
		data, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	data := build("v0.1.0", arch)
	otherArch := "amd64"
	if arch == otherArch {
		otherArch = "arm64"
	}
	wrongArch := build("v0.1.0", otherArch)
	run := func(want string, ok bool, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, "/bin/bash", append([]string{"install.sh"}, args...)...)
		cmd.Env = append(os.Environ(), "HOME="+base, "NO_COLOR=1")
		out, err := cmd.CombinedOutput()
		if (err == nil) != ok || !strings.Contains(string(out), want) {
			t.Fatalf("installer %v: %v\n%s; want success=%t, %q", args, err, out, ok, want)
		}
		if strings.Contains(string(out), "\x1b") {
			t.Fatal("unexpected terminal escapes")
		}
		return string(out)
	}
	targetDir := filepath.Join(base, "path with spaces", "bin")
	archive, manifest := releaseFixture(t, base, arch, "v0.1.0", []archiveEntry{{"oli", tar.TypeReg, data}})
	args := []string{"--version", "v0.1.0", "--bin-dir", targetDir, "--archive", archive, "--checksums", manifest}
	run("No downloads or writes", true, append(args, "--dry-run")...)
	if _, err := os.Stat(targetDir); !os.IsNotExist(err) {
		t.Fatal("preview created destination")
	}
	run("Installed oli v0.1.0", true, args...)
	target := filepath.Join(targetDir, "oli")
	installed, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(installed, data) {
		t.Fatalf("installed bytes differ: %v", err)
	}
	for _, command := range []string{"version", "help"} {
		out, err := exec.CommandContext(ctx, target, command).CombinedOutput()
		if err != nil || !strings.Contains(string(out), "oli") {
			t.Fatalf("installed %s: %v\n%s", command, err, out)
		}
	}
	run("already exists", false, args...)
	run("Installed oli v0.1.0", true, append([]string{"update", "--replace"}, args...)...)

	for _, tc := range []struct {
		name, version, want string
		entries             []archiveEntry
		manifestChange      string
	}{
		{name: "checksum mismatch", want: "checksum mismatch", entries: []archiveEntry{{"oli", tar.TypeReg, data}}, manifestChange: "bad-hash"},
		{name: "duplicate checksum", want: "exactly one checksum", entries: []archiveEntry{{"oli", tar.TypeReg, data}}, manifestChange: "duplicate"},
		{name: "missing checksum", want: "exactly one checksum", entries: []archiveEntry{{"oli", tar.TypeReg, data}}, manifestChange: "missing"},
		{name: "extra member", want: "only oli", entries: []archiveEntry{{"oli", tar.TypeReg, data}, {"extra", tar.TypeReg, []byte("no")}}},
		{name: "traversal", want: "only oli", entries: []archiveEntry{{"../escape", tar.TypeReg, []byte("no")}}},
		{name: "symlink member", want: "regular file", entries: []archiveEntry{{"oli", tar.TypeSymlink, nil}}},
		{name: "hardlink member", want: "regular file", entries: []archiveEntry{{"oli", tar.TypeLink, nil}}},
		{name: "duplicate member", want: "only oli", entries: []archiveEntry{{"oli", tar.TypeReg, data}, {"oli", tar.TypeReg, data}}},
		{name: "non executable payload", want: "architecture mismatch", entries: []archiveEntry{{"oli", tar.TypeReg, []byte("#!/bin/sh\nexit 0\n")}}},
		{name: "wrong architecture", want: "architecture mismatch", entries: []archiveEntry{{"oli", tar.TypeReg, wrongArch}}},
		{name: "version mismatch", version: "v0.1.1", want: "version mismatch", entries: []archiveEntry{{"oli", tar.TypeReg, data}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			version := tc.version
			if version == "" {
				version = "v0.1.0"
			}
			a, m := releaseFixture(t, t.TempDir(), arch, version, tc.entries)
			text, err := os.ReadFile(m)
			if err != nil {
				t.Fatal(err)
			}
			switch tc.manifestChange {
			case "bad-hash":
				text = []byte(strings.Repeat("0", 64) + string(text[64:]))
			case "duplicate":
				text = append(text, text...)
			case "missing":
				text = []byte("not a manifest\n")
			}
			if err := os.WriteFile(m, text, 0o600); err != nil {
				t.Fatal(err)
			}
			run(tc.want, false, "update", "--replace", "--version", version, "--bin-dir", targetDir, "--archive", a, "--checksums", m)
			got, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(got, installed) {
				t.Fatalf("failed update changed old installation: %v", err)
			}
			stages, _ := filepath.Glob(filepath.Join(targetDir, ".oli-install.*"))
			if len(stages) != 0 {
				t.Fatalf("leaked staging directories: %v", stages)
			}
		})
	}
	upgraded := build("v0.1.1", arch)
	upgradeArchive, upgradeManifest := releaseFixture(t, base, arch, "v0.1.1", []archiveEntry{{"oli", tar.TypeReg, upgraded}})
	run("Installed oli v0.1.1", true, "update", "--replace", "--version", "v0.1.1", "--bin-dir", targetDir, "--archive", upgradeArchive, "--checksums", upgradeManifest)
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, upgraded) {
		t.Fatalf("different-version update failed: %v", err)
	}
	link := filepath.Join(base, "linked-bin")
	if err := os.Symlink(targetDir, link); err != nil {
		t.Fatal(err)
	}
	run("symlinked installation", false, "--bin-dir", link, "--dry-run")
	if err := os.Chmod(targetDir, 0o777); err != nil {
		t.Fatal(err)
	}
	run("group/world writable", false, "--bin-dir", targetDir, "--replace", "--dry-run")
	if err := os.Chmod(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	run("dot path components", false, "--bin-dir", base+"/../bin", "--dry-run")
	run("invalid release version", false, "--version", "v0.1.0/../../evil", "--dry-run")
	run("requires --yes", false, "uninstall", "--bin-dir", targetDir)
	run("Would remove only", true, "uninstall", "--bin-dir", targetDir, "--dry-run")
	run("Removed", true, "uninstall", "--bin-dir", targetDir, "--yes")
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("uninstall retained binary")
	}
	if _, err := os.Stat(targetDir); err != nil {
		t.Fatal("uninstall removed directory")
	}
	run("no installation", false, "uninstall", "--bin-dir", targetDir, "--yes")
	if err := os.Symlink(binary, target); err != nil {
		t.Fatal(err)
	}
	run("symlink at the destination", false, "--bin-dir", targetDir, "--replace", "--dry-run")
}

type archiveEntry struct {
	name string
	kind byte
	data []byte
}

func releaseFixture(t *testing.T, dir, arch, version string, entries []archiveEntry) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		h := &tar.Header{Name: entry.name, Typeflag: entry.kind, Mode: 0o755, Size: int64(len(entry.data))}
		if entry.kind == tar.TypeSymlink || entry.kind == tar.TypeLink {
			h.Linkname = "outside"
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("oli-%s-darwin-%s.tar.gz", version, arch)
	archive := filepath.Join(dir, name)
	manifest := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(archive, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256(buf.Bytes()), name)
	if err := os.WriteFile(manifest, []byte(checksum), 0o600); err != nil {
		t.Fatal(err)
	}
	return archive, manifest
}
