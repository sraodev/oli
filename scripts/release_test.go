package scripts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestReleaseAssetsE2E(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("release artifacts are macOS binaries")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	assets := filepath.Join(t.TempDir(), "release")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	run := func(script string, wantSuccess bool) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, "/bin/bash", script, "v0.1.0", assets)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if (err == nil) != wantSuccess {
			t.Fatalf("%s: %v\n%s", script, err, out)
		}
		return string(out)
	}
	run("scripts/package-release.sh", true)
	if out := run("scripts/verify-release.sh", true); !strings.Contains(out, "Verified exact release assets") {
		t.Fatal(out)
	}
	manifest := filepath.Join(assets, "SHA256SUMS")
	original, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, append(original, []byte("0  unexpected\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if out := run("scripts/verify-release.sh", false); !strings.Contains(out, "unexpected entries") {
		t.Fatal(out)
	}
	if err := os.WriteFile(manifest, original, 0600); err != nil {
		t.Fatal(err)
	}
	installer := filepath.Join(assets, "install.sh")
	if err := os.WriteFile(installer, []byte("#!/bin/bash\nexit 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if out := run("scripts/verify-release.sh", false); !strings.Contains(out, "FAILED") {
		t.Fatal(out)
	}
}
