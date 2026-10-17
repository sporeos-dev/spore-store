package local

import (
	"fmt"
	"io"
	"net/http"
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
	catalogURL = "https://raw.githubusercontent.com/sporeos-dev/spore-store/main/store/catalog.yaml"
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

}

//
// get
//

func (c *Cache) Get(req *request.Request) {

}
