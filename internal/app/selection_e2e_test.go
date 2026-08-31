package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sraodev/mac-cleanup-studio/internal/cleanup"
	"github.com/sraodev/mac-cleanup-studio/internal/dashboard"
)

// Real HTTP -> service -> private plan -> filesystem. Only temporary fixtures.
func TestCandidateHTTPWorkflowEndToEnd(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	for _, name := range []string{"a", "b", "c"} {
		writeFixture(t, filepath.Join(root, name), name)
	}
	e, err := cleanup.New(home, []cleanup.Rule{{ID: "test", Name: "Test", Risk: cleanup.RiskSafe, Action: cleanup.ActionClean, Default: true, Auto: true, MinimumAge: 1, Roots: []string{root}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewDashboardService(e, home)
	if err != nil {
		t.Fatal(err)
	}
	const token = "0123456789abcdef0123456789abcdef"
	handler, err := dashboard.Handler(dashboard.Config{Token: token, Service: s})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	post := func(path string, body any, status int) []byte {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err = io.ReadAll(response.Body)
		if err != nil || response.StatusCode != status {
			t.Fatalf("%s status=%d body=%s err=%v", path, response.StatusCode, data, err)
		}
		return data
	}
	scan := func() (string, []dashboard.ScanItem) {
		data := post("/api/scan", dashboard.ScanRequest{Profile: "safe"}, 200)
		var scanID string
		var items []dashboard.ScanItem
		decoder := json.NewDecoder(bytes.NewReader(data))
		for decoder.More() {
			var event dashboard.ScanEvent
			if err := decoder.Decode(&event); err != nil {
				t.Fatal(err)
			}
			if event.Category != nil {
				items = append(items, event.Category.LargestItems...)
			}
			if event.Summary != nil {
				scanID = event.Summary.ScanID
			}
		}
		if scanID == "" || len(items) != 3 {
			t.Fatalf("scan=%s items=%+v", data, items)
		}
		return scanID, items
	}
	oldScan, oldItems := scan()
	scanID, items := scan()
	post("/api/selection", dashboard.SelectionRequest{ScanID: oldScan, CandidateIDs: []string{oldItems[0].CandidateID}}, 409)
	post("/api/selection", dashboard.SelectionRequest{ScanID: scanID, CandidateIDs: []string{oldItems[0].CandidateID}}, 409)
	var selected dashboard.ScanItem
	for _, item := range items {
		if item.Name == "~/cache/b" {
			selected = item
		}
	}
	request := dashboard.SelectionRequest{ScanID: scanID, CandidateIDs: []string{selected.CandidateID}}
	data := post("/api/selection", request, 200)
	var preview cleanup.Selection
	if err := json.Unmarshal(data, &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Candidates) != 1 || preview.Metrics.LogicalBytes != 1 || uint64(preview.Metrics.AllocatedBytes) != selected.SizeBytes {
		t.Fatalf("preview=%+v item=%+v", preview, selected)
	}
	data = post("/api/clean", dashboard.CleanRequest{ScanID: scanID, CandidateIDs: request.CandidateIDs, Confirmation: "DELETE"}, 200)
	var summary *dashboard.CleanSummary
	decoder := json.NewDecoder(bytes.NewReader(data))
	for decoder.More() {
		var event dashboard.CleanEvent
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event.Issue != nil {
			t.Fatalf("cleanup issue: %+v", event.Issue)
		}
		if event.Summary != nil {
			summary = event.Summary
		}
	}
	if summary == nil || summary.RemovedItems != 1 || summary.RemovedBytes != selected.SizeBytes {
		t.Fatalf("summary=%+v", summary)
	}
	for _, name := range []string{"a", "c"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "b")); !os.IsNotExist(err) {
		t.Fatalf("selected file remains: %v", err)
	}
	post("/api/selection", request, 409)
	data = post("/api/clean", dashboard.CleanRequest{ScanID: scanID, CandidateIDs: request.CandidateIDs, Confirmation: "DELETE"}, 200)
	if !bytes.Contains(data, []byte(`"clean.issue"`)) || bytes.Contains(data, []byte(`"clean.complete"`)) {
		t.Fatalf("replay=%s", data)
	}
	// Protection added after review wins at the mutation boundary through HTTP.
	data = post("/api/scan", dashboard.ScanRequest{Profile: "safe"}, 200)
	decoder = json.NewDecoder(bytes.NewReader(data))
	var nextID, candidateID string
	for decoder.More() {
		var event dashboard.ScanEvent
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event.Summary != nil {
			nextID = event.Summary.ScanID
		}
		if event.Category != nil {
			candidateID = event.Category.LargestItems[0].CandidateID
		}
	}
	writeFixture(t, filepath.Join(home, cleanup.ProtectionConfig), `{"version":1,"rules":["test"]}`)
	data = post("/api/clean", dashboard.CleanRequest{ScanID: nextID, CandidateIDs: []string{candidateID}, Confirmation: "DELETE"}, 200)
	if !bytes.Contains(data, []byte(`"partial":true`)) || !bytes.Contains(data, []byte(`"failed_items":1`)) {
		t.Fatalf("fresh protection result=%s", data)
	}
	for _, name := range []string{"a", "c"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal("protection failed", err)
		}
	}
	writeFixture(t, filepath.Join(home, cleanup.ProtectionConfig), `{`)
	data = post("/api/scan", dashboard.ScanRequest{Profile: "safe"}, 200)
	if !bytes.Contains(data, []byte(`"code":"protection_unavailable"`)) || bytes.Contains(data, []byte(`"scan.complete"`)) {
		t.Fatalf("corrupt policy=%s", data)
	}
}
