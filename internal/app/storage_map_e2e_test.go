package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sraodev/mac-cleanup-studio/internal/cleanup"
	"github.com/sraodev/mac-cleanup-studio/internal/dashboard"
)

func TestStorageMapHTTPReadOnlyEndToEnd(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "Downloads", "project", "nested", "keep.txt")
	writeFixture(t, path, "sentinel")
	writeFixture(t, filepath.Join(home, "Downloads", "closed", "keep"), "unknown")
	if err := os.Chmod(filepath.Join(home, "Downloads", "closed"), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(home, "Downloads", "closed"), 0700) })
	e, err := cleanup.NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewDashboardService(e, home)
	if err != nil {
		t.Fatal(err)
	}
	const token = "map-fixture-authorization-token"
	h, err := dashboard.Handler(dashboard.Config{Token: token, Service: s})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	post := func(path, body string, authorized bool) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest("POST", server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if authorized {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, data
	}
	if status, _ := post("/api/explore", `{"scope":"downloads"}`, false); status != http.StatusUnauthorized {
		t.Fatalf("auth=%d", status)
	}
	if status, _ := post("/api/explore", `{"scope":"downloads","path":"/"}`, true); status != http.StatusBadRequest {
		t.Fatalf("path accepted=%d", status)
	}
	status, data := post("/api/explore", `{"scope":"downloads"}`, true)
	if status != 200 {
		t.Fatalf("%d %s", status, data)
	}
	var r cleanup.Exploration
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if r.Action != cleanup.ActionScanOnly || s.current != nil || !r.Partial || !r.MapNodes[0].Partial {
		t.Fatalf("unsafe/incomplete report %+v", r)
	}
	var id string
	for _, node := range r.MapNodes {
		if node.DisplayPath == "~/Downloads/project" {
			id = node.ID
		}
	}
	children := cleanup.FilterMap(r.MapNodes, id, cleanup.MapFilter{Extension: "txt"}, 100)
	if len(children) != 1 || children[0].Metrics.LogicalBytes != 8 {
		t.Fatalf("nested map=%+v", children)
	}
	_, data = post("/api/clean", `{"scan_id":"map-0","candidate_ids":["`+children[0].ID+`"],"confirmation":"DELETE"}`, true)
	if !bytes.Contains(data, []byte(`"invalid_scan"`)) || bytes.Contains(data, []byte(`"clean.complete"`)) {
		t.Fatalf("map granted deletion: %s", data)
	}
	if contents, err := os.ReadFile(path); err != nil || string(contents) != "sentinel" {
		t.Fatal("personal fixture changed")
	}
	resp, err := server.Client().Get(server.URL + "/assets/map.js")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "javascript") {
		t.Fatal("map script missing")
	}
}
