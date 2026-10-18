package local

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sporeos-dev/spore-client-libs/spore_go/response"
	"go.yaml.in/yaml/v3"
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

func TestDetailsAPIOmitsOperationalMetadata(t *testing.T) {
	details := DetailsOut{
		API: []APIOperation{{
			Name:        "example",
			Description: "An example operation.",
			Usage:       []string{"example"},
		}},
	}

	data, err := yaml.Marshal(details)
	if err != nil {
		t.Fatalf("marshal details: %v", err)
	}

	output := string(data)
	for _, field := range []string{"risk:", "inputs:", "outputs:", "notes:"} {
		if strings.Contains(output, field) {
			t.Errorf("details API includes removed field %q:\n%s", field, output)
		}
	}
}

func TestFindNodeAcceptsUniqueSuffixes(t *testing.T) {
	nodes := []Node{{ID: "dev.sporeos.foundry.alias"}}

	for _, identifier := range []string{
		"dev.sporeos.foundry.alias",
		"sporeos.foundry.alias",
		"foundry.alias",
		"alias",
		"foundry-alias",
	} {
		node, matches := findNode(nodes, identifier)
		if node == nil || matches != 1 || node.ID != nodes[0].ID {
			t.Fatalf("findNode(%q) = node=%#v matches=%d, want the unique node", identifier, node, matches)
		}
	}
}

func TestFindNodeRejectsAmbiguousSuffix(t *testing.T) {
	nodes := []Node{
		{ID: "dev.sporeos.foundry.alias"},
		{ID: "dev.sporeos.other.foundry.alias"},
	}

	node, matches := findNode(nodes, "alias")
	if node != nil || matches != 2 {
		t.Fatalf("findNode(alias) = node=%#v matches=%d, want collision", node, matches)
	}

	node, matches = findNode(nodes, "foundry-alias")
	if node != nil || matches != 2 {
		t.Fatalf("findNode(foundry-alias) = node=%#v matches=%d, want collision", node, matches)
	}
}

func TestExtractArchivePlacesManifestAndRejectsTraversal(t *testing.T) {
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "node.zip")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	writer := zip.NewWriter(archive)
	manifest, err := writer.Create("example.manifest.spore.yaml")
	if err != nil {
		t.Fatalf("create manifest: %v", err)
	}
	if _, err := manifest.Write([]byte("id: dev.sporeos.example\n")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive writer: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	destination := filepath.Join(directory, "store", "example")
	if err := extractArchive(archivePath, destination); err != nil {
		t.Fatalf("extract archive: %v", err)
	}
	manifestPath := filepath.Join(destination, "example.manifest.spore.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest was not extracted at %q: %v", manifestPath, err)
	}

	traversalPath := filepath.Join(directory, "traversal.zip")
	traversal, err := os.Create(traversalPath)
	if err != nil {
		t.Fatalf("create traversal archive: %v", err)
	}
	traversalWriter := zip.NewWriter(traversal)
	entry, err := traversalWriter.Create("../outside")
	if err != nil {
		t.Fatalf("create traversal entry: %v", err)
	}
	if _, err := entry.Write([]byte("unsafe")); err != nil {
		t.Fatalf("write traversal entry: %v", err)
	}
	if err := traversalWriter.Close(); err != nil {
		t.Fatalf("close traversal writer: %v", err)
	}
	if err := traversal.Close(); err != nil {
		t.Fatalf("close traversal archive: %v", err)
	}

	if err := extractArchive(traversalPath, destination); err == nil {
		t.Fatal("extract archive with traversal path succeeded")
	}
}

func TestVerifySHA256RejectsInvalidAndMismatchedChecksums(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact")
	if err := os.WriteFile(path, []byte("expected contents"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	const expected = "sha256:532e548a772806301a0a36d9e3d04b7b38e8d55e80edbeb750cc187907d76287"
	if err := verifySHA256(path, expected); err != nil {
		t.Fatalf("verify matching checksum: %v", err)
	}
	if err := verifySHA256(path, "sha256:0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("verify mismatched checksum succeeded")
	}
	if err := verifySHA256(path, "md5:abc"); err == nil {
		t.Fatal("verify unsupported checksum succeeded")
	}
}
