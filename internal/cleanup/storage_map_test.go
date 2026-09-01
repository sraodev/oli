package cleanup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStorageMapHierarchyAccountingAndFilters(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "Downloads", "project", "nested", "report.pdf"), strings.Repeat("x", 100))
	mustWrite(t, filepath.Join(home, "Downloads", "project", "notes.txt"), "notes")
	mustWrite(t, filepath.Join(home, "Downloads", ".hidden"), "hidden")
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(home, "Downloads", "project", "nested", "report.pdf"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(home, "Downloads", "project", "notes.txt"), filepath.Join(home, "Downloads", "alias.txt")); err != nil {
		t.Fatal(err)
	}
	e, err := NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Explore(context.Background(), ExploreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Total.LogicalBytes != 111 || r.Partial {
		t.Fatalf("report=%+v", r)
	}
	byID := map[string]MapNode{}
	byPath := map[string]MapNode{}
	aliases := 0
	for _, node := range r.MapNodes {
		byID[node.ID] = node
		byPath[node.DisplayPath] = node
		if node.Kind == "hardlink" {
			aliases++
		}
	}
	if aliases != 1 {
		t.Fatalf("aliases=%d", aliases)
	}
	for _, node := range r.MapNodes {
		if node.ID != "map-0" {
			if _, ok := byID[node.ParentID]; !ok {
				t.Fatalf("orphan=%+v", node)
			}
		}
		if node.Kind != "directory" {
			continue
		}
		var sum int64
		for _, child := range r.MapNodes {
			if child.ParentID == node.ID {
				sum += child.Metrics.LogicalBytes
			}
		}
		if sum+node.UnmappedLogicalBytes != node.Metrics.LogicalBytes {
			t.Fatalf("double counted node=%+v sum=%d", node, sum)
		}
	}
	before, _ := json.Marshal(r)
	project := byPath["~/Downloads/project"]
	if children := FilterMap(r.MapNodes, project.ID, MapFilter{}, 200); len(children) != 2 {
		t.Fatalf("children=%+v", children)
	}
	filter := MapFilter{Search: "REPORT", Kind: "file", Extension: "PDF", MinBytes: 100, MaxBytes: 100, Before: old.Add(time.Second), After: old}
	got := FilterMap(r.MapNodes, project.ID, filter, 200)
	if len(got) != 1 || got[0].DisplayPath != "~/Downloads/project/nested/report.pdf" {
		t.Fatalf("matches=%+v", got)
	}
	filter.Before = old
	if len(FilterMap(r.MapNodes, project.ID, filter, 200)) != 0 {
		t.Fatal("before date was inclusive")
	}
	after, _ := json.Marshal(r)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("filter mutated snapshot totals")
	}
	if strings.Contains(string(after), home) {
		t.Fatal("absolute home leaked")
	}
}

func TestStorageMapBoundAccountsUnmappedMeasuredBytes(t *testing.T) {
	m := newMapBuilder()
	scope := m.enter("~/Downloads", time.Time{})
	for i := 0; i < StorageMapLimit+10; i++ {
		m.add("~/Downloads/file", "file", time.Time{}, Metrics{LogicalBytes: 1, AllocatedBytes: 2})
	}
	total := Metrics{LogicalBytes: StorageMapLimit + 10, AllocatedBytes: 2 * (StorageMapLimit + 10)}
	m.leave(scope, total)
	nodes := m.finish(total)
	if len(nodes) != StorageMapLimit || m.omitted != 12 || nodes[scope].UnmappedLogicalBytes != 12 || nodes[scope].UnmappedAllocatedBytes != 24 {
		t.Fatalf("len=%d omitted=%d scope=%+v", len(nodes), m.omitted, nodes[scope])
	}
	if nodes[0].UnmappedLogicalBytes != 0 {
		t.Fatal("parent double counted omitted bytes")
	}
}

func TestStorageMapWideTraversalKeepsTotalsBeyondDisplayCap(t *testing.T) {
	home := t.TempDir()
	for i := 0; i < StorageMapLimit+20; i++ {
		mustWrite(t, filepath.Join(home, "Downloads", fmt.Sprintf("file-%04d", i)), "x")
	}
	e, err := NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Explore(context.Background(), ExploreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Partial || r.Total.LogicalBytes != StorageMapLimit+20 || len(r.MapNodes) != StorageMapLimit || r.MapNodesOmitted != 22 || r.MapNodes[1].UnmappedLogicalBytes != 22 {
		t.Fatalf("partial=%t total=%d nodes=%d omitted=%d remainder=%d", r.Partial, r.Total.LogicalBytes, len(r.MapNodes), r.MapNodesOmitted, r.MapNodes[1].UnmappedLogicalBytes)
	}
}

func TestStorageMapPartialDeepEmptyAndCancellation(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "Downloads", "keep"), "keep")
	path := filepath.Join(home, "Downloads", "deep")
	for i := 0; i < 66; i++ {
		path = filepath.Join(path, "d")
	}
	mustWrite(t, filepath.Join(path, "never"), "outside depth bound")
	e, err := NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Explore(context.Background(), ExploreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Partial || !r.MapNodes[0].Partial || r.Total.LogicalBytes != 4 {
		t.Fatalf("partial=%t total=%d", r.Partial, r.Total.LogicalBytes)
	}
	r, err = e.Explore(context.Background(), ExploreOptions{entryLimit: 1})
	if err != nil || !r.Partial || !r.MapNodes[0].Partial {
		t.Fatalf("bounded=%+v err=%v", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = e.Explore(ctx, ExploreOptions{}); err != context.Canceled {
		t.Fatalf("cancel=%v", err)
	}
	r, err = e.Explore(context.Background(), ExploreOptions{Scope: "documents"})
	if err != nil || r.Partial || len(r.MapNodes) != 2 || r.Total.LogicalBytes != 0 {
		t.Fatalf("empty=%+v err=%v", r, err)
	}
}
