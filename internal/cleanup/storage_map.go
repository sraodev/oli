package cleanup

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

const StorageMapLimit = 5000

// MapNode IDs are navigation labels within this report, never cleanup IDs.
type MapNode struct {
	ID                     string    `json:"id"`
	ParentID               string    `json:"parent_id"`
	DisplayPath            string    `json:"display_path"`
	Kind                   string    `json:"kind"`
	Modified               time.Time `json:"modified"`
	Metrics                Metrics   `json:"metrics"`
	Partial                bool      `json:"partial"`
	UnmappedAllocatedBytes int64     `json:"unmapped_allocated_bytes"`
	UnmappedLogicalBytes   int64     `json:"unmapped_logical_bytes"`
}

type mapBuilder struct {
	nodes   []MapNode
	stack   []int
	omitted int
}

func newMapBuilder() *mapBuilder {
	return &mapBuilder{nodes: []MapNode{{ID: "map-0", DisplayPath: "Selected scopes", Kind: "directory"}}, stack: []int{0}}
}

func (m *mapBuilder) add(display, kind string, modified time.Time, metrics Metrics) int {
	if m == nil {
		return -1
	}
	parent := m.stack[len(m.stack)-1]
	if parent < 0 || len(m.nodes) >= StorageMapLimit {
		m.omitted++
		return -1
	}
	index := len(m.nodes)
	m.nodes = append(m.nodes, MapNode{ID: fmt.Sprintf("map-%d", index), ParentID: m.nodes[parent].ID, DisplayPath: display, Kind: kind, Modified: modified.UTC(), Metrics: metrics})
	return index
}

func (m *mapBuilder) enter(display string, modified time.Time) int {
	if m == nil {
		return -1
	}
	index := m.add(display, "directory", modified, Metrics{})
	m.stack = append(m.stack, index)
	return index
}

func (m *mapBuilder) leave(index int, total Metrics) {
	if m == nil {
		return
	}
	if index >= 0 {
		m.nodes[index].Metrics = total
	}
	m.stack = m.stack[:len(m.stack)-1]
}

func (m *mapBuilder) warn() {
	if m == nil {
		return
	}
	for _, index := range m.stack {
		if index >= 0 {
			m.nodes[index].Partial = true
		}
	}
}

func (m *mapBuilder) finish(total Metrics) []MapNode {
	m.nodes[0].Metrics = total
	children := map[string]Metrics{}
	for _, node := range m.nodes {
		metrics := children[node.ParentID]
		addMetrics(&metrics, node.Metrics)
		children[node.ParentID] = metrics
	}
	for i := range m.nodes {
		node := &m.nodes[i]
		if node.Kind == "directory" {
			node.UnmappedAllocatedBytes = max(0, node.Metrics.AllocatedBytes-children[node.ID].AllocatedBytes)
			node.UnmappedLogicalBytes = max(0, node.Metrics.LogicalBytes-children[node.ID].LogicalBytes)
		}
	}
	return m.nodes
}

// MapFilter changes presentation only; it never changes the measured tree.
type MapFilter struct {
	Search    string
	Kind      string
	Extension string
	MinBytes  int64
	MaxBytes  int64
	Before    time.Time
	After     time.Time
}

func (f MapFilter) Active() bool {
	return f.Search != "" || f.Kind != "" && f.Kind != "all" || f.Extension != "" || f.MinBytes > 0 || f.MaxBytes > 0 || !f.Before.IsZero() || !f.After.IsZero()
}

// FilterMap selects descendants when filtering, direct children otherwise.
// Parent identity is resolved only in the supplied snapshot, never on disk.
func FilterMap(nodes []MapNode, parentID string, f MapFilter, limit int) []MapNode {
	parents := map[string]string{}
	for _, node := range nodes {
		parents[node.ID] = node.ParentID
	}
	result := []MapNode{}
	for _, node := range nodes {
		parent := node.ParentID
		if f.Active() {
			for steps := 0; parent != "" && parent != parentID && steps < len(nodes); steps++ {
				parent = parents[parent]
			}
		}
		if parent != parentID || node.ID == parentID {
			continue
		}
		if f.Search != "" && !strings.Contains(strings.ToLower(node.DisplayPath), strings.ToLower(f.Search)) {
			continue
		}
		if f.Kind != "" && f.Kind != "all" && node.Kind != f.Kind {
			continue
		}
		if f.Extension != "" && (node.Kind != "file" || !strings.EqualFold(path.Ext(node.DisplayPath), "."+strings.TrimPrefix(f.Extension, "."))) {
			continue
		}
		if node.Metrics.LogicalBytes < f.MinBytes || f.MaxBytes > 0 && node.Metrics.LogicalBytes > f.MaxBytes {
			continue
		}
		if (!f.Before.IsZero() || !f.After.IsZero()) && node.Modified.IsZero() {
			continue
		}
		if !f.Before.IsZero() && !node.Modified.Before(f.Before) || !f.After.IsZero() && node.Modified.Before(f.After) {
			continue
		}
		result = append(result, node)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Metrics.AllocatedBytes != result[j].Metrics.AllocatedBytes {
			return result[i].Metrics.AllocatedBytes > result[j].Metrics.AllocatedBytes
		}
		return result[i].DisplayPath < result[j].DisplayPath
	})
	if limit > 0 && len(result) > limit {
		return result[:limit]
	}
	return result
}
