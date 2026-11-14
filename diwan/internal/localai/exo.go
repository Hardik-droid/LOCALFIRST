// Package localai integrates an independently managed Exo service. It never
// imports document storage, collaboration transports, or native process runners.
package localai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"time"
)

const ExoCommit = "a60b95698ca81737d367443674dd5563cbc9559b"
const maxMetadata = 8 << 20

type Memory struct {
	Bytes int64 `json:"inBytes"`
}
type Node struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	OS            string             `json:"os"`
	Chip          string             `json:"chip"`
	Backends      []string           `json:"backends"`
	AvailableRAM  *Memory            `json:"availableRam"`
	AvailableDisk *Memory            `json:"availableDisk"`
	Interfaces    []NetworkInterface `json:"interfaces"`
	LastSeen      *time.Time         `json:"lastSeen"`
}
type NetworkInterface struct {
	Name string `json:"name"`
	Type string `json:"interfaceType"`
}
type Model struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	StorageMB int64    `json:"storage_size_megabytes"`
	Tasks     []string `json:"tasks"`
}
type ModelCard struct {
	ID       string   `json:"modelId"`
	Storage  Memory   `json:"storageSize"`
	Memory   *Memory  `json:"memorySize"`
	Backends []string `json:"backends"`
}
type Instance struct {
	ID            string   `json:"id"`
	Model         string   `json:"model"`
	Nodes         []string `json:"nodes"`
	ResidentBytes int64    `json:"residentBytes"`
}
type Snapshot struct {
	Gateway    string     `json:"gateway"`
	Nodes      []Node     `json:"nodes"`
	Models     []Model    `json:"models"`
	Instances  []Instance `json:"instances"`
	ObservedAt time.Time  `json:"observedAt"`
}
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Completion struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream"`
}

// Provider is intentionally read-only except for user-requested inference.
// Joining nodes, placing models, downloads and arbitrary API paths are excluded.
type Provider interface {
	Snapshot(context.Context) (Snapshot, error)
	Complete(context.Context, Completion) (*http.Response, error)
	Close()
}
type Exo struct {
	base   string
	client *http.Client
}

func (e *Exo) Close() { e.client.CloseIdleConnections() }

func NewExo(endpoint string) (*Exo, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("use an http://numeric-loopback:port Exo endpoint, directly or through an authenticated SSH tunnel")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || u.Port() == "" {
		return nil, errors.New("Exo endpoints must use a numeric loopback address and explicit port; LAN/public endpoints are not allowed")
	}
	if _, err := net.ResolveTCPAddr("tcp", u.Host); err != nil {
		return nil, errors.New("invalid Exo port")
	}
	u.Path = ""
	transport := &http.Transport{
		Proxy:           nil, // Never inherit a proxy that could receive private prompts.
		DialContext:     (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		MaxConnsPerHost: 4, MaxIdleConnsPerHost: 2, IdleConnTimeout: 15 * time.Second,
		ResponseHeaderTimeout: 45 * time.Second, MaxResponseHeaderBytes: 32 << 10,
	}
	return &Exo{base: u.String(), client: &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("Exo redirects are not allowed") }}}, nil
}

func (e *Exo) read(ctx context.Context, path string, value any) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", e.base+path, nil)
	res, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("Exo unavailable: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("Exo metadata request failed (HTTP %d)", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxMetadata+1))
	if err != nil {
		return err
	}
	if len(data) > maxMetadata {
		return errors.New("Exo metadata exceeds the 8 MiB limit")
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("incompatible Exo response: %w", err)
	}
	return nil
}

func (e *Exo) Snapshot(ctx context.Context) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	s := Snapshot{Nodes: []Node{}, Models: []Model{}, Instances: []Instance{}, ObservedAt: time.Now().UTC()}
	if err := e.read(ctx, "/node_id", &s.Gateway); err != nil {
		return s, err
	}
	if s.Gateway == "" || len(s.Gateway) > 256 {
		return s, errors.New("Exo did not report a valid gateway identity")
	}
	var state struct {
		Identities map[string]struct {
			Name string `json:"friendlyName"`
			OS   string `json:"osVersion"`
			Chip string `json:"chipId"`
		} `json:"nodeIdentities"`
		Memory map[string]struct {
			Available Memory `json:"ramAvailable"`
		} `json:"nodeMemory"`
		Disk map[string]struct {
			Available Memory `json:"available"`
		} `json:"nodeDisk"`
		Backends map[string][]string `json:"nodeBackends"`
		Network  map[string]struct {
			Interfaces []NetworkInterface `json:"interfaces"`
		} `json:"nodeNetwork"`
		LastSeen  map[string]time.Time `json:"lastSeen"`
		Instances map[string]map[string]struct {
			Assignments struct {
				Model  string            `json:"modelId"`
				Nodes  map[string]string `json:"nodeToRunner"`
				Shards map[string]struct {
					Card ModelCard `json:"modelCard"`
				} `json:"runnerToShard"`
			} `json:"shardAssignments"`
		} `json:"instances"`
	}
	if err := e.read(ctx, "/state", &state); err != nil {
		return s, err
	}
	if state.Identities == nil || state.Instances == nil {
		return s, errors.New("incompatible Exo state: expected nodeIdentities and instances")
	}
	for id, identity := range state.Identities {
		n := Node{ID: id, Name: identity.Name, OS: identity.OS, Chip: identity.Chip, Backends: state.Backends[id], Interfaces: state.Network[id].Interfaces}
		if m, ok := state.Memory[id]; ok {
			n.AvailableRAM = &m.Available
		}
		if d, ok := state.Disk[id]; ok {
			n.AvailableDisk = &d.Available
		}
		if seen, ok := state.LastSeen[id]; ok {
			n.LastSeen = &seen
		}
		s.Nodes = append(s.Nodes, n)
	}
	for id, tagged := range state.Instances {
		if len(tagged) != 1 {
			return s, errors.New("incompatible Exo instance schema")
		}
		for kind, raw := range tagged {
			if kind != "MlxRingInstance" && kind != "MlxJacclInstance" {
				return s, errors.New("unsupported Exo instance type")
			}
			a := raw.Assignments
			i := Instance{ID: id, Model: a.Model, Nodes: []string{}}
			for node := range a.Nodes {
				i.Nodes = append(i.Nodes, node)
			}
			sort.Strings(i.Nodes)
			for _, shard := range a.Shards {
				resident := shard.Card.Storage.Bytes
				if shard.Card.Memory != nil {
					resident = shard.Card.Memory.Bytes
				}
				if resident > i.ResidentBytes {
					i.ResidentBytes = resident
				}
			}
			s.Instances = append(s.Instances, i)
		}
	}
	var models struct {
		Data []Model `json:"data"`
	}
	if err := e.read(ctx, "/models?status=downloaded", &models); err != nil {
		return s, err
	}
	if models.Data == nil {
		return s, errors.New("incompatible Exo model list: expected data array")
	}
	s.Models = models.Data
	sort.Slice(s.Nodes, func(i, j int) bool { return s.Nodes[i].ID < s.Nodes[j].ID })
	sort.Slice(s.Instances, func(i, j int) bool { return s.Instances[i].ID < s.Instances[j].ID })
	return s, nil
}

func (e *Exo) Complete(ctx context.Context, input Completion) (*http.Response, error) {
	input.Stream = true
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", e.base+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	return e.client.Do(req)
}
