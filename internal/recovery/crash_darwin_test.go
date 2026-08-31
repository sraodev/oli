//go:build darwin

package recovery

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This is a process-kill platform probe, not a production recovery journal.
// A fresh process observes whether a prepared record and exactly one payload
// survive at each boundary. It never claims sudden-power-loss durability.
func TestExclusiveMoveCrashBoundaries(t *testing.T) {
	stages := []string{"prepared-written", "prepared-synced", "before-move", "after-rename", "source-synced", "destination-synced", "committed-written", "committed-synced"}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			for _, name := range []string{"source", "record"} {
				if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
					t.Fatal(err)
				}
			}
			write(t, filepath.Join(root, "source", "candidate"), "fixture-payload")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := crashProbe(ctx, root, stage)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			// Always reap a child, including when it fails to reach the barrier.
			defer func() { _ = cmd.Process.Kill() }()
			scanner := bufio.NewScanner(stdout)
			reached := false
			for scanner.Scan() {
				if scanner.Text() == "READY "+stage {
					reached = true
					break
				}
			}
			if !reached {
				_ = cmd.Wait()
				t.Fatalf("barrier %s not reached: %s", stage, stderr.String())
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil {
				t.Fatal("probe did not terminate abnormally")
			}
			// Another fresh process reopens directories and reports observed state.
			output, err := crashProbe(ctx, root, "inspect").CombinedOutput()
			if err != nil {
				t.Fatalf("restart inspection: %v %s", err, output)
			}
			var state struct {
				Source, Payload bool
				Journal         string
			}
			line := strings.SplitN(string(output), "\n", 2)[0]
			if err := json.Unmarshal([]byte(line), &state); err != nil {
				t.Fatalf("inspect=%s err=%v", output, err)
			}
			if state.Source == state.Payload {
				t.Fatalf("lost/duplicated payload: %+v", state)
			}
			wantSource := stage == "prepared-written" || stage == "prepared-synced" || stage == "before-move"
			if state.Source != wantSource {
				t.Fatalf("state=%+v want source=%v", state, wantSource)
			}
			wantJournal := "prepared"
			if stage == "committed-written" || stage == "committed-synced" {
				wantJournal = "quarantined"
			}
			if state.Journal != wantJournal {
				t.Fatalf("journal=%q want=%q", state.Journal, wantJournal)
			}
		})
	}
}

func crashProbe(ctx context.Context, root, stage string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecoveryPlatformProbeProcess$")
	cmd.Env = append(os.Environ(), "MCS_RECOVERY_PROBE_ROOT="+root, "MCS_RECOVERY_PROBE_STAGE="+stage)
	return cmd
}

func TestRecoveryPlatformProbeProcess(t *testing.T) {
	root, stage := os.Getenv("MCS_RECOVERY_PROBE_ROOT"), os.Getenv("MCS_RECOVERY_PROBE_STAGE")
	if root == "" || stage == "" {
		t.Skip("subprocess fixture helper")
	}
	if stage == "inspect" {
		source, payload := false, false
		for i, path := range []string{filepath.Join(root, "source", "candidate"), filepath.Join(root, "record", "payload")} {
			data, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || string(data) != "fixture-payload" {
				t.Fatalf("payload=%q err=%v", data, err)
			}
			if i == 0 {
				source = true
			} else {
				payload = true
			}
		}
		journal, err := os.ReadFile(filepath.Join(root, "record", "journal"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(struct {
			Source, Payload bool
			Journal         string
		}{source, payload, string(journal)}); err != nil {
			t.Fatal(err)
		}
		return
	}
	barrier := func(name string) {
		if name != stage {
			return
		}
		fmt.Println("READY " + name)
		// Parent owns this pipe and kills us while we are blocked here.
		if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
			t.Fatal(err)
		}
		t.Fatal("probe resumed instead of being killed")
	}
	src := directory(t, filepath.Join(root, "source"))
	dst := directory(t, filepath.Join(root, "record"))
	journalPath := filepath.Join(root, "record", "journal")
	journal, err := os.OpenFile(journalPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if _, err := journal.WriteString("prepared"); err != nil {
		t.Fatal(err)
	}
	barrier("prepared-written")
	if err := journal.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := dst.Sync(); err != nil {
		t.Fatal(err)
	}
	barrier("prepared-synced")
	barrier("before-move")
	calls := 0
	result, err := exclusiveMoveWithSync(src, "candidate", dst, "payload", func(parent *os.File) error {
		calls++
		if calls == 1 {
			barrier("after-rename")
		}
		if err := parent.Sync(); err != nil {
			return err
		}
		if calls == 1 {
			barrier("source-synced")
		} else {
			barrier("destination-synced")
		}
		return nil
	})
	if err != nil || !result.Durable {
		t.Fatalf("move=%+v err=%v", result, err)
	}
	// The old record is retained until a complete, synced replacement exists.
	temp := filepath.Join(root, "record", "journal.next")
	next, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err := next.WriteString("quarantined"); err != nil {
		t.Fatal(err)
	}
	if err := next.Sync(); err != nil {
		t.Fatal(err)
	}
	// Metadata replacement only; payload movement always uses exclusive rename.
	if err := os.Rename(temp, journalPath); err != nil {
		t.Fatal(err)
	}
	barrier("committed-written")
	if err := dst.Sync(); err != nil {
		t.Fatal(err)
	}
	barrier("committed-synced")
	t.Fatal("unknown crash barrier")
}
