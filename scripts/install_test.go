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
	t.Run("curl bootstrap", func(t *testing.T) { testCurlBootstrap(t, ctx, base, arch, data) })
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

func testCurlBootstrap(t *testing.T, ctx context.Context, base, arch string, data []byte) {
	t.Helper()
	root := filepath.Join(base, "curl-fixture")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	archive, manifest := releaseFixture(t, root, arch, "v0.1.0", []archiveEntry{{"oli", tar.TypeReg, data}})
	source, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	// Substitute only the transport in a test copy; production keeps its fixed
	// system curl and official release origin, with no runtime test bypass.
	curl := filepath.Join(root, "curl")
	shim := `#!/bin/bash
set -eu
[[ $1 == --disable ]] || exit 90
args=" $* "
[[ $args == *" --proto =https "* && $args == *" --proto-redir =https "* && $args == *" --tlsv1.2 "* ]] || exit 91
[[ $args == *" --connect-timeout 15 "* && $args == *" --max-time "* ]] || exit 92
output=
while [[ $# -gt 0 ]]; do
  case $1 in --output) output=$2; shift ;; esac
  url=$1
  shift
done
printf '%s\n' "$url" >> "$OLI_TEST_LOG"
case $url in
  https://github.com/sraodev/oli/releases/latest)
    case $OLI_TEST_FAILURE in
      latest) exit 22 ;;
      redirect) printf 'https://example.invalid/releases/tag/v0.1.0'; exit 0 ;;
    esac
    printf 'https://github.com/sraodev/oli/releases/tag/v0.1.0' ;;
  https://github.com/sraodev/oli/releases/download/v0.1.0/SHA256SUMS)
    /bin/cp "$OLI_TEST_MANIFEST" "$output" ;;
  https://github.com/sraodev/oli/releases/download/v0.1.0/oli-v0.1.0-darwin-*.tar.gz)
    case $OLI_TEST_FAILURE in
      timeout) exit 28 ;;
      partial) printf 'partial' > "$output"; exit 18 ;;
      checksum) printf 'corrupt' > "$output"; exit 0 ;;
    esac
    /bin/cp "$OLI_TEST_ARCHIVE" "$output" ;;
  *) exit 93 ;;
esac
`
	if err := os.WriteFile(curl, []byte(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	transportSource := strings.ReplaceAll(string(source), "/usr/bin/curl", "\""+curl+"\"")
	targetDir := filepath.Join(root, "installed bin")
	log := filepath.Join(root, "requests")
	run := func(script, failure string, ok bool, want string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "/bin/bash", append([]string{"-s", "--"}, args...)...)
		cmd.Stdin = strings.NewReader(script)
		cmd.Env = append(os.Environ(), "HOME="+root, "OLI_TEST_ARCHIVE="+archive, "OLI_TEST_MANIFEST="+manifest, "OLI_TEST_LOG="+log, "OLI_TEST_FAILURE="+failure)
		out, err := cmd.CombinedOutput()
		if (err == nil) != ok || !strings.Contains(string(out), want) {
			t.Fatalf("piped installer %v (%s): %v\n%s", args, failure, err, out)
		}
	}
	// Cut off before the guarded entry point. No partial download may execute
	// an install, even if it already contains mutation commands.
	cut := strings.Index(transportSource, "/bin/mv -f")
	if cut < 0 {
		t.Fatal("missing install commit boundary")
	}
	run(transportSource[:cut], "", false, "syntax error", "--install-dir", targetDir)
	run(transportSource, "", true, "No downloads or writes", "--install-dir", targetDir, "--no-modify-path", "--dry-run")
	for _, p := range []string{targetDir, log} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("truncated script or preview touched %s: %v", p, err)
		}
	}
	run(transportSource, "", true, "Installed oli v0.1.0", "--install-dir", targetDir, "--no-modify-path")
	requests, err := os.ReadFile(log)
	if err != nil || strings.Count(string(requests), "/releases/latest") != 1 || strings.Count(string(requests), "/download/v0.1.0/") != 2 {
		t.Fatalf("latest was not resolved once to pinned assets: %v\n%s", err, requests)
	}
	for _, failure := range []string{"latest", "redirect", "timeout", "partial", "checksum"} {
		t.Run(failure, func(t *testing.T) {
			want := ""
			if failure == "latest" {
				want = "cannot resolve a published release"
			}
			run(transportSource, failure, false, want, "update", "--replace", "--install-dir", targetDir)
			got, err := os.ReadFile(filepath.Join(targetDir, "oli"))
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("failed transport changed installed bytes: %v", err)
			}
			stages, _ := filepath.Glob(filepath.Join(targetDir, ".oli-install.*"))
			if len(stages) != 0 {
				t.Fatalf("failed transport leaked staging: %v", stages)
			}
		})
	}
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	run(transportSource, "latest", true, "Installed oli v0.1.0", "update", "--replace", "--version", "v0.1.0", "--install-dir", targetDir)
	requests, err = os.ReadFile(log)
	if err != nil || strings.Contains(string(requests), "/releases/latest") {
		t.Fatalf("pinned install requested latest: %v", err)
	}
	run(transportSource, "", true, "Removed", "uninstall", "--yes", "--install-dir", targetDir)
	for _, name := range []string{".zshrc", ".bashrc", ".profile"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("installer touched shell profile %s", name)
		}
	}
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
