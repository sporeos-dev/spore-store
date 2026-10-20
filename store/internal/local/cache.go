// Copyright 2026 Matt Harrison
// SPDX-License-Identifier: Apache-2.0

package local

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	spore "github.com/sporeos-dev/spore-client-libs/spore_go"
	"github.com/sporeos-dev/spore-client-libs/spore_go/request"
	"github.com/sporeos-dev/spore-client-libs/spore_go/response"
	"github.com/sporeos-dev/spore-client-libs/spore_go/witness"
	"go.yaml.in/yaml/v3"
)

type Cache struct {
	client  *spore.Client
	Catalog `yaml:",inline"`

	sync.RWMutex
}

const (
	catalogURL  = "https://raw.githubusercontent.com/sporeos-dev/spore-store/main/store/catalog.yaml"
	manifestURL = "https://raw.githubusercontent.com/sporeos-dev/spore-store/main/store/manifests/"
	githubAPI   = "https://api.github.com"
)

type Catalog struct {
	RefreshDate string `yaml:"date"`
	RefreshTime string `yaml:"time"`
	Nodes       []Node `yaml:"nodes"`
}

type Node struct {
	Repo        string    `yaml:"repo"`
	State       string    `yaml:"state"`
	Manifest    string    `yaml:"manifest"`
	Product     string    `yaml:"product"`
	ID          string    `yaml:"id"`
	Name        string    `yaml:"name"`
	Version     string    `yaml:"version"`
	Description string    `yaml:"description"`
	Checksum    string    `yaml:"checksum"`
	Date        string    `yaml:"date"`
	Time        string    `yaml:"time"`
	Releases    []Release `yaml:"releases"`
}

type Release struct {
	OpSys     string     `yaml:"opsys"`
	Arch      string     `yaml:"arch"`
	Artifacts []Artifact `yaml:"artifacts"`
}

type Artifact struct {
	Name     string `yaml:"name"`
	Checksum string `yaml:"checksum"`
}

func New(client *spore.Client) *Cache {
	return &Cache{
		client: client,
	}
}

func (c *Cache) Load() {
	req := request.New("dev.sporeos.SPORE.state.retrieve", "load-cache").
		WithArg("key", "state")
	c.client.SendRequest(req)
}

func (c *Cache) OnLoad(res *response.Response, err *response.ResponseError) {
	if res == nil {
		return
	}
	handle := res.Handle()
	if handle != "load-cache" {
		return
	}
	if res.Flag("empty") {
		return
	}

	body, ok := res.Arg("body")
	if !ok {
		c.client.SendWitness(
			witness.New("missing 'body'"))
		return
	}

	c.Lock()
	defer c.Unlock()

	sperr := yaml.Unmarshal([]byte(body), c)
	if sperr != nil {
		c.client.SendWitness(
			witness.New("unmarshalling error").
				WithArg("err", sperr.Error()))
	}
}

//
// refresh
//

func (c *Cache) Refresh(req *request.Request) {
	if !strings.HasSuffix(req.Command(), "refresh") {
		return
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(catalogURL)
	if err != nil {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "RefreshFailed", err.Error()))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		c.client.SendResponseError(
			response.Error(
				req.Command(),
				req.Handle(),
				"RefreshFailed",
				fmt.Sprintf("unexpected HTTP status %s", resp.Status)))
		return
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "RefreshFailed", err.Error()))
		return
	}

	c.Lock()
	if err := yaml.Unmarshal(data, &c.Catalog); err != nil {
		c.client.SendResponseError(
			response.Error(
				req.Command(),
				req.Handle(),
				"RefreshFailed",
				fmt.Sprintf("parse catalog: %v", err)))
		c.Unlock()
		return
	}
	c.RefreshDate = time.Now().Format("2006-01-02")
	c.RefreshTime = time.Now().Format("15:04:05")
	c.Unlock()

	body, err := yaml.Marshal(&c.Catalog)
	if err != nil {
		c.client.SendResponseError(
			response.Error(
				req.Command(),
				req.Handle(),
				"RefreshFailed",
				fmt.Sprintf("marshal catalog: %v", err)))
		return
	}

	res := response.New(req.Command(), req.Handle())
	c.client.SendResponse(res)

	saveReq := request.New(
		"dev.sporeos.SPORE.state.persist",
		"save-cache").
		WithArg("key", "state").
		WithArg("body", string(body))
	c.client.SendRequest(saveReq)
}

//
// browse
//

type BrowseOut struct {
	Nodes []BrowseNode `yaml:"nodes"`
}

type BrowseNode struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func (c *Cache) Browse(req *request.Request) {
	if !strings.HasSuffix(req.Command(), "browse") {
		return
	}

	nodes, err := yaml.Marshal(c.browseNodes())
	if err != nil {
		c.client.SendResponseError(
			response.Error(
				req.Command(),
				req.Handle(),
				"BrowseFailed",
				fmt.Sprintf("marshal nodes: %v", err)))
		return
	}

	res := response.New(req.Command(), req.Handle()).
		WithArg("nodes", string(nodes))
	c.client.SendResponse(res)
}

func (c *Cache) browseNodes() []BrowseNode {
	c.RLock()
	defer c.RUnlock()

	var nodes []BrowseNode
	for _, node := range c.Nodes {
		for _, release := range node.Releases {
			if isOpSys(release.OpSys) &&
				isArch(release.Arch) {
				nodes = append(nodes, BrowseNode{
					ID:          node.ID,
					Name:        node.Name,
					Description: node.Description,
				})
				break
			}
		}
	}

	return nodes
}

func isOpSys(catalog string) bool {
	runtime := runtime.GOOS
	return catalog == runtime ||
		(catalog == "macos" && runtime == "darwin")
}

func isArch(catalog string) bool {
	runtime := runtime.GOARCH
	return catalog == runtime ||
		(catalog == "x64" && runtime == "amd64")
}

//
// details
//

func (c *Cache) Details(req *request.Request) {
	if !strings.HasSuffix(req.Command(), "details") {
		return
	}

	nodeID, ok := req.Arg("node")
	if !ok {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "DetailsFailed", "missing 'node' argument"))
		return
	}

	c.RLock()
	node, matches := findNode(c.Nodes, nodeID)
	c.RUnlock()

	if node == nil {
		if matches > 1 {
			c.client.SendResponseError(
				response.Error(req.Command(), req.Handle(), "DetailsCollision",
					fmt.Sprintf("node identifier %q matches %d nodes", nodeID, matches)))
			return
		}
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "DetailsFailed",
				fmt.Sprintf("node %q was not found", nodeID)))
		return
	}
	if node.Manifest == "" {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "DetailsFailed",
				"catalog entry is missing manifest location information"))
		return
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(manifestURL + url.PathEscape(node.Manifest))
	if err != nil {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "DetailsFailed", err.Error()))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		c.client.SendResponseError(
			response.Error(
				req.Command(),
				req.Handle(),
				"DetailsFailed",
				fmt.Sprintf("unexpected HTTP status %s", resp.Status)))
		return
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "DetailsFailed", err.Error()))
		return
	}

	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		c.client.SendResponseError(
			response.Error(
				req.Command(),
				req.Handle(),
				"DetailsFailed",
				fmt.Sprintf("parse manifest: %v", err)))
		return
	}

	details := DetailsOut{
		ID:          manifest.ID,
		Name:        manifest.Name,
		Description: manifest.Description,
		Version:     manifest.Version,
		Trust:       manifest.Trust,
		Roles:       manifest.Roles,
		API:         manifest.API,
	}
	if details.ID == "" {
		details.ID = node.ID
	}
	if details.Name == "" {
		details.Name = node.Name
	}
	if details.Description == "" {
		details.Description = node.Description
	}
	if details.Version == "" {
		details.Version = node.Version
	}

	detailsYAML, err := yaml.Marshal(&details)
	if err != nil {
		c.client.SendResponseError(
			response.Error(
				req.Command(),
				req.Handle(),
				"DetailsFailed",
				fmt.Sprintf("marshal details: %v", err)))
		return
	}

	res := response.New(req.Command(), req.Handle()).
		WithArg("details", string(detailsYAML))
	c.client.SendResponse(res)

}

func findNode(nodes []Node, identifier string) (*Node, int) {
	identifier = normalizeNodeIdentifier(identifier)
	if identifier == "" {
		return nil, 0
	}

	var match *Node
	matches := 0
	for i := range nodes {
		if !nodeMatchesIdentifier(nodes[i].ID, identifier) {
			continue
		}
		matches++
		if matches == 1 {
			copy := nodes[i]
			match = &copy
		}
	}
	if matches != 1 {
		return nil, matches
	}
	return match, matches
}

func nodeMatchesIdentifier(nodeID string, identifier string) bool {
	parts := strings.Split(normalizeNodeIdentifier(nodeID), ".")
	for i := range parts {
		if strings.Join(parts[i:], ".") == identifier {
			return true
		}
	}
	return false
}

func normalizeNodeIdentifier(identifier string) string {
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	identifier = strings.ReplaceAll(identifier, "-", ".")
	return strings.Trim(identifier, ".")
}

type DetailsOut struct {
	ID          string         `yaml:"id"`
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Version     string         `yaml:"version"`
	Trust       string         `yaml:"trust"`
	Roles       []string       `yaml:"roles"`
	API         []APIOperation `yaml:"api"`
}

type Manifest struct {
	ID          string         `yaml:"id"`
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Version     string         `yaml:"version"`
	Trust       string         `yaml:"trust"`
	Roles       []string       `yaml:"roles"`
	API         []APIOperation `yaml:"api"`
}

type APIOperation struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Usage       []string `yaml:"usage"`
}

//
// get
//

func (c *Cache) Get(req *request.Request) {
	if !strings.HasSuffix(req.Command(), "get") {
		return
	}

	nodeID, ok := req.Arg("node")
	if !ok {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "GetFailed", "missing 'node' argument"))
		return
	}

	c.RLock()
	node, matches := findNode(c.Nodes, nodeID)
	c.RUnlock()

	if node == nil {
		if matches > 1 {
			c.client.SendResponseError(
				response.Error(req.Command(), req.Handle(), "GetCollision",
					fmt.Sprintf("node identifier %q matches %d nodes", nodeID, matches)))
			return
		}
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "GetFailed",
				fmt.Sprintf("node %q was not found", nodeID)))
		return
	}

	release, artifact, err := node.platformArtifact()
	if err != nil {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "GetFailed", err.Error()))
		return
	}

	manifestName, err := safeFilename(node.Manifest)
	if err != nil {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "GetFailed",
				fmt.Sprintf("invalid manifest name: %v", err)))
		return
	}
	artifactName, err := safeFilename(artifact.Name)
	if err != nil {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "GetFailed",
				fmt.Sprintf("invalid artifact name: %v", err)))
		return
	}

	archiveName := fmt.Sprintf("%s-%s-%s.zip", artifactName, release.OpSys, release.Arch)
	archiveURL, err := releaseAssetURL(node.Repo, archiveName)
	if err != nil {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "GetFailed", err.Error()))
		return
	}

	storeDir, err := platformStoreDirectory()
	if err != nil {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "GetFailed", err.Error()))
		return
	}
	nodeDir := filepath.Join(storeDir, artifactName)
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "GetFailed",
				fmt.Sprintf("create node directory: %v", err)))
		return
	}

	archivePath := filepath.Join(nodeDir, archiveName)
	if err := downloadArchive(archiveURL, archivePath); err != nil {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "GetFailed",
				fmt.Sprintf("download archive: %v", err)))
		return
	}
	defer os.Remove(archivePath)

	if err := extractArchive(archivePath, nodeDir); err != nil {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "GetFailed",
				fmt.Sprintf("extract archive: %v", err)))
		return
	}

	manifestPath := filepath.Join(nodeDir, manifestName)
	if err := verifySHA256(manifestPath, node.Checksum); err != nil {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "GetFailed",
				fmt.Sprintf("verify manifest checksum: %v", err)))
		return
	}

	artifactPath := filepath.Join(nodeDir, artifactName)
	if err := verifySHA256(artifactPath, artifact.Checksum); err != nil {
		c.client.SendResponseError(
			response.Error(req.Command(), req.Handle(), "GetFailed",
				fmt.Sprintf("verify artifact checksum: %v", err)))
		return
	}

	c.client.SendResponse(
		response.New(req.Command(), req.Handle()).WithArg("path", manifestPath))
}

func (n *Node) platformArtifact() (*Release, *Artifact, error) {
	var matched *Release
	for i := range n.Releases {
		release := &n.Releases[i]
		if !isOpSys(release.OpSys) || !isArch(release.Arch) {
			continue
		}
		if matched != nil {
			return nil, nil, fmt.Errorf("node %q has multiple releases for %s/%s",
				n.ID, runtime.GOOS, runtime.GOARCH)
		}
		matched = release
	}
	if matched == nil || len(matched.Artifacts) != 1 {
		artifacts := 0
		if matched != nil {
			artifacts = len(matched.Artifacts)
		}
		return nil, nil, fmt.Errorf("node %q has %d artifacts for %s/%s, want exactly one",
			n.ID, artifacts, runtime.GOOS, runtime.GOARCH)
	}
	return matched, &matched.Artifacts[0], nil
}

type githubRelease struct {
	Assets []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func releaseAssetURL(repo string, archiveName string) (string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("invalid release repository %q", repo)
	}

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Get(
		fmt.Sprintf("%s/repos/%s/%s/releases?per_page=100",
			githubAPI, url.PathEscape(parts[0]), url.PathEscape(parts[1])))
	if err != nil {
		return "", fmt.Errorf("list releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("list releases: unexpected HTTP status %s", resp.Status)
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("decode releases: %w", err)
	}
	for _, release := range releases {
		for _, asset := range release.Assets {
			if asset.Name == archiveName && asset.BrowserDownloadURL != "" {
				return asset.BrowserDownloadURL, nil
			}
		}
	}
	return "", fmt.Errorf("release asset %q was not found in %s", archiveName, repo)
}

func platformStoreDirectory() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/spore-os/store", nil
	case "linux":
		return "/var/lib/spore-os/store", nil
	default:
		return "", fmt.Errorf("unsupported operating system %q", runtime.GOOS)
	}
}

func safeFilename(name string) (string, error) {
	if name == "" || filepath.Base(name) != name || name == "." {
		return "", fmt.Errorf("%q is not a file name", name)
	}
	return name, nil
}

func downloadArchive(archiveURL string, archivePath string) (err error) {
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Get(archiveURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}

	archive, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := archive.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	_, err = io.Copy(archive, resp.Body)
	return err
}

func extractArchive(archivePath string, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		name := filepath.Clean(file.Name)
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("archive contains invalid path %q", file.Name)
		}

		path := filepath.Join(destination, name)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(path, file.Mode()); err != nil {
				return err
			}
			continue
		}
		if !file.FileInfo().Mode().IsRegular() {
			return fmt.Errorf("archive contains unsupported file %q", file.Name)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := extractFile(file, path); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(file *zip.File, path string) (err error) {
	source, err := file.Open()
	if err != nil {
		return err
	}
	defer func() {
		closeErr := source.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	mode := file.Mode()
	if mode == 0 {
		mode = 0o644
	}
	destination, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := destination.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	_, err = io.Copy(destination, source)
	return err
}

func verifySHA256(path string, expected string) error {
	const prefix = "sha256:"

	if !strings.HasPrefix(expected, prefix) {
		return fmt.Errorf("unsupported checksum %q", expected)
	}
	expected = strings.TrimPrefix(expected, prefix)
	if len(expected) != sha256.Size*2 {
		return fmt.Errorf("invalid SHA-256 checksum %q", expected)
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	checksum := sha256.New()
	if _, err := io.Copy(checksum, file); err != nil {
		return err
	}
	actual := fmt.Sprintf("%x", checksum.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: got sha256:%s, want sha256:%s", actual, expected)
	}
	return nil
}
