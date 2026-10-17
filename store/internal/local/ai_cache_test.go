package local

import (
	"runtime"
	"testing"

	"github.com/sporeos-dev/spore-client-libs/spore_go/response"
)

func TestOnLoadLoadsInlineCatalog(t *testing.T) {
	cache := New(nil)
	res := response.New("dev.sporeos.SPORE.state.retrieve", "load-cache").
		WithArg("body", `
date: "2026-10-17"
time: "11:02:01"
nodes:
  - id: dev.sporeos.example
    name: Example
    description: An example node.
    releases:
      - opsys: macos
        arch: arm64
`)

	cache.OnLoad(res, nil)

	if cache.RefreshDate != "2026-10-17" || cache.RefreshTime != "11:02:01" {
		t.Fatalf("cache metadata was not loaded: date=%q time=%q", cache.RefreshDate, cache.RefreshTime)
	}
	if len(cache.Nodes) != 1 || cache.Nodes[0].ID != "dev.sporeos.example" {
		t.Fatalf("cache nodes were not loaded: %#v", cache.Nodes)
	}
}

func TestBrowseNodesFiltersForCurrentPlatform(t *testing.T) {
	cache := &Cache{
		Catalog: Catalog{
			Nodes: []Node{
				{
					ID:          "dev.sporeos.available",
					Name:        "Available",
					Description: "Available on this platform.",
					Releases: []Release{{
						OpSys: runtime.GOOS,
						Arch:  runtime.GOARCH,
					}},
				},
				{
					ID: "dev.sporeos.unavailable",
					Releases: []Release{{
						OpSys: "unsupported-os",
						Arch:  "unsupported-arch",
					}},
				},
			},
		},
	}

	nodes := cache.browseNodes()

	if len(nodes) != 1 {
		t.Fatalf("browse returned %d nodes, want 1: %#v", len(nodes), nodes)
	}
	if nodes[0].ID != "dev.sporeos.available" {
		t.Fatalf("browse returned node %q, want %q", nodes[0].ID, "dev.sporeos.available")
	}
}
