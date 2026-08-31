//go:build darwin

package recovery

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func directory(t *testing.T, path string) *os.File {
	t.Helper()
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	f := os.NewFile(uintptr(fd), path)
	t.Cleanup(func() { f.Close() })
	return f
}

func fixture(t *testing.T) (string, *os.File, string, *os.File) {
	t.Helper()
	a, b := t.TempDir(), t.TempDir()
	return a, directory(t, a), b, directory(t, b)
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func unchanged(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("%s: data=%q err=%v", path, data, err)
	}
}

func TestExclusiveMoveRoundTrip(t *testing.T) {
	for _, kind := range []string{"file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			a, src, b, dst := fixture(t)
			name := filepath.Join(a, "candidate")
			dataPath := name
			if kind == "directory" {
				if err := os.Mkdir(name, 0700); err != nil {
					t.Fatal(err)
				}
				dataPath = filepath.Join(name, "data")
			}
			write(t, dataPath, "preserved")
			before, err := os.Lstat(dataPath)
			if err != nil {
				t.Fatal(err)
			}
			result, err := exclusiveMove(src, "candidate", dst, "payload")
			if err != nil || !result.Moved || !result.Durable {
				t.Fatalf("move=%+v err=%v", result, err)
			}
			if _, err := os.Lstat(name); !os.IsNotExist(err) {
				t.Fatalf("source remains: %v", err)
			}
			result, err = exclusiveMove(dst, "payload", src, "candidate")
			if err != nil || !result.Durable {
				t.Fatalf("restore=%+v err=%v", result, err)
			}
			unchanged(t, dataPath, "preserved")
			after, err := os.Lstat(dataPath)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("round trip changed identity/mode/mtime")
			}
			if _, err := os.Lstat(filepath.Join(b, "payload")); !os.IsNotExist(err) {
				t.Fatal("payload remains after restore")
			}
		})
	}
}

func TestExclusiveMoveNeverOverwrites(t *testing.T) {
	for _, sourceKind := range []string{"file", "directory"} {
		for _, kind := range []string{"file", "empty-directory", "nonempty-directory", "symlink", "dangling-symlink"} {
			t.Run(sourceKind+"-to-"+kind, func(t *testing.T) {
				a, src, b, dst := fixture(t)
				dataPath := filepath.Join(a, "candidate")
				if sourceKind == "directory" {
					if err := os.Mkdir(dataPath, 0700); err != nil {
						t.Fatal(err)
					}
					dataPath = filepath.Join(dataPath, "data")
				}
				write(t, dataPath, "source")
				target := filepath.Join(b, "payload")
				outside := filepath.Join(t.TempDir(), "outside")
				write(t, outside, "outside")
				switch kind {
				case "file":
					write(t, target, "existing")
				case "empty-directory", "nonempty-directory":
					if err := os.Mkdir(target, 0700); err != nil {
						t.Fatal(err)
					}
					if kind == "nonempty-directory" {
						write(t, filepath.Join(target, "data"), "existing")
					}
				case "symlink":
					if err := os.Symlink(outside, target); err != nil {
						t.Fatal(err)
					}
				case "dangling-symlink":
					if err := os.Symlink(outside+"-missing", target); err != nil {
						t.Fatal(err)
					}
				}
				before, err := os.Lstat(target)
				if err != nil {
					t.Fatal(err)
				}
				result, err := exclusiveMove(src, "candidate", dst, "payload")
				if err == nil || result.Moved {
					t.Fatalf("overwrote %s: %+v err=%v", kind, result, err)
				}
				after, err := os.Lstat(target)
				if err != nil || !os.SameFile(before, after) {
					t.Fatal("destination replaced")
				}
				unchanged(t, dataPath, "source")
				unchanged(t, outside, "outside")
				if kind == "file" {
					unchanged(t, target, "existing")
				}
				if kind == "nonempty-directory" {
					unchanged(t, filepath.Join(target, "data"), "existing")
				}
			})
		}
	}
}

func TestExclusiveMovePreservesLinksAndExtendedAttributes(t *testing.T) {
	a, src, b, dst := fixture(t)
	path := filepath.Join(a, "candidate")
	write(t, path, "shared")
	alias := filepath.Join(a, "alias")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	const attribute = "com.maccleanupstudio.test"
	if err := unix.Setxattr(path, attribute, []byte("metadata"), 0); err != nil {
		t.Fatal(err)
	}
	result, err := exclusiveMove(src, "candidate", dst, "payload")
	if err != nil || !result.Durable {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	payload := filepath.Join(b, "payload")
	info, err := os.Stat(payload)
	if err != nil {
		t.Fatal(err)
	}
	linkInfo, err := os.Stat(alias)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(info, linkInfo) {
		t.Fatal("move broke hardlink identity")
	}
	data := make([]byte, 32)
	n, err := unix.Getxattr(payload, attribute, data)
	if err != nil || string(data[:n]) != "metadata" {
		t.Fatalf("xattr=%q err=%v", data[:n], err)
	}
	// A hard-link alias can still mutate quarantined bytes: recovery must verify
	// manifests again before restore/purge, not claim immutable storage.
	write(t, alias, "changed through alias")
	unchanged(t, payload, "changed through alias")
	if err := os.Symlink(alias, filepath.Join(a, "link")); err != nil {
		t.Fatal(err)
	}
	result, err = exclusiveMove(src, "link", dst, "link")
	if err != nil || !result.Durable {
		t.Fatalf("symlink move=%+v err=%v", result, err)
	}
	target, err := os.Readlink(filepath.Join(b, "link"))
	if err != nil || target != alias {
		t.Fatalf("link=%q err=%v", target, err)
	}
	unchanged(t, alias, "changed through alias")
}

func TestExclusiveMoveRejectsPaths(t *testing.T) {
	_, src, _, dst := fixture(t)
	for _, name := range []string{"", ".", "..", "../outside", "/absolute", "a/b", "a\x00b"} {
		if result, err := exclusiveMove(src, name, dst, "payload"); err == nil || result.Moved {
			t.Fatalf("source path %q accepted", name)
		}
		if result, err := exclusiveMove(src, "candidate", dst, name); err == nil || result.Moved {
			t.Fatalf("destination path %q accepted", name)
		}
	}
}

func TestExclusiveMovePinsParentHandles(t *testing.T) {
	a, src, b, dst := fixture(t)
	write(t, filepath.Join(a, "candidate"), "source")
	outside := t.TempDir()
	write(t, filepath.Join(outside, "payload"), "outside")
	moved := b + "-moved"
	if err := os.Rename(b, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(b); os.Rename(moved, b) })
	if err := os.Symlink(outside, b); err != nil {
		t.Fatal(err)
	}
	result, err := exclusiveMove(src, "candidate", dst, "payload")
	if err != nil || !result.Durable {
		t.Fatalf("move=%+v err=%v", result, err)
	}
	unchanged(t, filepath.Join(moved, "payload"), "source")
	unchanged(t, filepath.Join(outside, "payload"), "outside")
}

func TestExclusiveMoveConcurrentCollision(t *testing.T) {
	for i := 0; i < 25; i++ {
		a, src, b, dst := fixture(t)
		write(t, filepath.Join(a, "one"), "one")
		write(t, filepath.Join(a, "two"), "two")
		var wg sync.WaitGroup
		results := make(chan moveOutcome, 2)
		for _, name := range []string{"one", "two"} {
			wg.Add(1)
			go func() { defer wg.Done(); outcome, _ := exclusiveMove(src, name, dst, "payload"); results <- outcome }()
		}
		wg.Wait()
		close(results)
		count := 0
		for result := range results {
			if result.Moved {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("winners=%d", count)
		}
		data, err := os.ReadFile(filepath.Join(b, "payload"))
		if err != nil {
			t.Fatal(err)
		}
		loser := "one"
		if string(data) == "one" {
			loser = "two"
		} else if string(data) != "two" {
			t.Fatal("corrupt payload")
		}
		unchanged(t, filepath.Join(a, loser), loser)
	}
}

func TestExclusiveMoveDurabilityFailureIsNotRenameFailure(t *testing.T) {
	a, src, b, dst := fixture(t)
	write(t, filepath.Join(a, "candidate"), "source")
	calls := 0
	result, err := exclusiveMoveWithSync(src, "candidate", dst, "payload", func(*os.File) error { calls++; return unix.EIO })
	if !errors.Is(err, unix.EIO) || !result.Moved || result.Durable || calls != 2 {
		t.Fatalf("result=%+v err=%v sync calls=%d", result, err, calls)
	}
	unchanged(t, filepath.Join(b, "payload"), "source")
	if _, err := os.Stat(filepath.Join(a, "candidate")); !os.IsNotExist(err) {
		t.Fatal("source unexpectedly retained")
	}
}

func TestExclusiveMoveCrossVolume(t *testing.T) {
	other := os.Getenv("MCS_RECOVERY_TEST_OTHER_VOLUME")
	if other == "" {
		t.Skip("requires operator-approved disposable APFS volume; not simulated")
	}
	a, src, _, _ := fixture(t)
	b, err := os.MkdirTemp(other, "mcs-cross-volume-")
	if err != nil {
		t.Fatal(err)
	}
	// This exact test-created directory contains only this test's fixture.
	t.Cleanup(func() { os.Remove(filepath.Join(b, "payload")); os.Remove(b) })
	dst := directory(t, b)
	write(t, filepath.Join(a, "candidate"), "source")
	result, err := exclusiveMove(src, "candidate", dst, "payload")
	if !errors.Is(err, ErrCrossVolume) || result.Moved {
		t.Fatalf("result=%+v err=%v; fixture must be a different APFS volume", result, err)
	}
	unchanged(t, filepath.Join(a, "candidate"), "source")
	if _, err := os.Lstat(filepath.Join(b, "payload")); !os.IsNotExist(err) {
		t.Fatal("cross-volume payload created")
	}
}
