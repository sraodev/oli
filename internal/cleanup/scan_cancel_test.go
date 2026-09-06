package cleanup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanCancellationAtFinalProgressDoesNotComplete(t *testing.T) {
	for _, boundary := range []EventKind{EventCandidateScanned, EventRuleCompleted} {
		t.Run(string(boundary), func(t *testing.T) {
			home := t.TempDir()
			root := filepath.Join(home, "cache")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "keep")
			if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			engine := mustEngine(t, home, testRule("test", root))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancelled, completed := false, false
			scan, err := engine.Scan(ctx, ScanOptions{Callback: func(event Event) {
				if event.Kind == boundary {
					cancelled = true
					cancel()
				}
				if event.Kind == EventScanCompleted {
					completed = true
				}
			}})
			if !cancelled || !errors.Is(err, context.Canceled) || scan != nil || completed {
				t.Fatalf("cancelled=%t error=%v scan returned=%t completion event=%t", cancelled, err, scan != nil, completed)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
				t.Fatalf("scan changed fixture: %q %v", data, err)
			}
		})
	}
}

func TestScanExpiredDeadlineDoesNotComplete(t *testing.T) {
	home := t.TempDir()
	engine := mustEngine(t, home, testRule("test", filepath.Join(home, "cache")))
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	completed := false
	scan, err := engine.Scan(ctx, ScanOptions{Callback: func(event Event) {
		completed = completed || event.Kind == EventScanCompleted
	}})
	if !errors.Is(err, context.DeadlineExceeded) || scan != nil || completed {
		t.Fatalf("error=%v scan returned=%t completion event=%t", err, scan != nil, completed)
	}
}

func TestScanCompletionCallbackIsAfterCancellationBoundary(t *testing.T) {
	home := t.TempDir()
	engine := mustEngine(t, home, testRule("test", filepath.Join(home, "cache")))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completions := 0
	scan, err := engine.Scan(ctx, ScanOptions{Callback: func(event Event) {
		if event.Kind == EventScanCompleted {
			completions++
			cancel()
		}
	}})
	if err != nil || scan == nil || completions != 1 {
		t.Fatalf("error=%v scan returned=%t completion count=%d", err, scan != nil, completions)
	}
}
