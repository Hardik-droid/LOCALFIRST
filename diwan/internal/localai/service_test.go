package localai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// These are HTTP contract fixtures derived from the pinned Exo schemas, NOT an
// inference engine. Passing these tests does not establish real model inference.
const stateFixture = `{"nodeIdentities":{"node-1":{"friendlyName":"contract fixture","osVersion":"fixture OS","chipId":"unknown"}},"nodeMemory":{"node-1":{"ramAvailable":{"inBytes":4294967296}}},"nodeDisk":{"node-1":{"available":{"inBytes":8589934592}}},"nodeBackends":{"node-1":["MlxCpu"]},"instances":{"instance-1":{"MlxRingInstance":{"shardAssignments":{"modelId":"fixture/model","nodeToRunner":{"node-1":"runner-1"},"runnerToShard":{"runner-1":{"modelCard":{"modelId":"fixture/model","storageSize":{"inBytes":1073741824}}}}}}}}}`

type fixture struct {
	server   *httptest.Server
	metadata atomic.Int32
	chats    atomic.Int32
	state    atomic.Value
	gateway  atomic.Value
}

func newFixture(t *testing.T, stream http.HandlerFunc) *fixture {
	t.Helper()
	f := &fixture{}
	f.state.Store(stateFixture)
	f.gateway.Store("fixture-gateway")
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			// A real Exo handler consumes its JSON input before generating.
			// Draining it also lets net/http observe a client-side disconnect.
			_, _ = io.Copy(io.Discard, r.Body)
			f.chats.Add(1)
			if stream != nil {
				stream(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"contract fixture only\"}}]}\n\ndata: [DONE]\n\n")
			return
		}
		f.metadata.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/node_id":
			_ = json.NewEncoder(w).Encode(f.gateway.Load())
		case "/state":
			_, _ = fmt.Fprint(w, f.state.Load().(string))
		case "/models":
			if r.URL.Query().Get("status") != "downloaded" {
				t.Error("adapter queried non-downloaded models")
			}
			_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"fixture/model","name":"fixture","storage_size_megabytes":1024,"tasks":["TextGeneration"]}]}`)
		default:
			t.Errorf("unapproved upstream route: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}
func request(s *Service, ctx context.Context, method, path string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	r := httptest.NewRequestWithContext(ctx, method, "http://127.0.0.1:8765/api/local-ai/"+path, strings.NewReader(string(data)))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-LocalFirst-AI-Token", s.token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func connectFixture(t *testing.T, s *Service, f *fixture, policy Policy) {
	t.Helper()
	w := request(s, context.Background(), "POST", "connect", map[string]any{"endpoint": f.server.URL, "gateway": "fixture-gateway", "consent": true, "policy": policy})
	if w.Code != 200 {
		t.Fatalf("connect: %d %s", w.Code, w.Body.String())
	}
	t.Cleanup(s.Close)
}
func chatInput() map[string]any {
	return map[string]any{"model": "fixture/model", "messages": []Message{{Role: "user", Content: "private prompt fixture"}}, "max_tokens": 16, "consent": true}
}
func approve(t *testing.T, s *Service) {
	t.Helper()
	w := request(s, context.Background(), "POST", "trust", map[string]any{"node": "node-1", "trusted": true})
	if w.Code != 200 {
		t.Fatalf("trust: %s", w.Body.String())
	}
}

func TestEndpointAndRequestSecurity(t *testing.T) {
	for _, endpoint := range []string{"https://127.0.0.1:52415", "http://example.com:52415", "http://192.168.1.5:52415", "http://localhost:52415", "http://user:secret@127.0.0.1:52415", "http://127.0.0.1:52415/path", "http://127.0.0.1:52415?q=x"} {
		if p, err := NewExo(endpoint); err == nil {
			p.Close()
			t.Errorf("accepted unsafe endpoint %s", endpoint)
		}
	}
	if LoopbackListener(":8080") || LoopbackListener("0.0.0.0:8080") || !LoopbackListener("127.0.0.1:8080") {
		t.Fatal("public listener isolation failed")
	}
	s := New()
	for _, attack := range []string{"origin", "host", "remote", "token", "fetch-site"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:8765/api/local-ai/status", nil)
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("X-LocalFirst-AI-Token", s.token)
		switch attack {
		case "origin":
			r.Header.Set("Origin", "http://evil.example")
		case "host":
			r.Host = "evil.example"
		case "remote":
			r.RemoteAddr = "192.168.1.5:12345"
		case "token":
			r.Header.Set("X-LocalFirst-AI-Token", "wrong")
		case "fetch-site":
			r.Header.Set("Sec-Fetch-Site", "same-site")
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("%s: got %d", attack, w.Code)
		}
	}
}
func TestDisabledAndUnavailableNeverStartInference(t *testing.T) {
	s := New()
	if w := request(s, context.Background(), "POST", "chat", chatInput()); w.Code != 409 {
		t.Fatalf("disabled: %d", w.Code)
	}
	if s.provider != nil || s.root != nil || s.enabled {
		t.Fatal("disabled service created background work")
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	endpoint := closed.URL
	closed.Close()
	w := request(s, context.Background(), "POST", "probe", map[string]any{"endpoint": endpoint})
	if w.Code != 503 || s.enabled {
		t.Fatalf("absent Exo: %d enabled=%v", w.Code, s.enabled)
	}
}
func TestPinnedExoContractTrustAndStreaming(t *testing.T) {
	f := newFixture(t, nil)
	s := New()
	connectFixture(t, s, f, DefaultPolicy())
	p, _ := NewExo(f.server.URL)
	defer p.Close()
	snapshot, err := p.Snapshot(context.Background())
	if err != nil || len(snapshot.Nodes) != 1 || snapshot.Nodes[0].AvailableRAM.Bytes != 4294967296 || len(snapshot.Instances) != 1 || snapshot.Instances[0].ResidentBytes != 1073741824 {
		t.Fatalf("schema parsing: %+v %v", snapshot, err)
	}
	if w := request(s, context.Background(), "POST", "chat", chatInput()); w.Code != 409 {
		t.Fatalf("untrusted inference: %d", w.Code)
	}
	if f.chats.Load() != 0 {
		t.Fatal("prompt sent before approval")
	}
	approve(t, s)
	w := request(s, context.Background(), "POST", "chat", chatInput())
	if w.Code != 200 || !strings.Contains(w.Body.String(), "contract fixture only") || !strings.Contains(w.Body.String(), "[DONE]") {
		t.Fatalf("stream: %d %s", w.Code, w.Body.String())
	}
	if s.jobs[len(s.jobs)-1].State != "completed" {
		t.Fatal("stream job was not completed")
	}
	before := f.metadata.Load()
	request(s, context.Background(), "POST", "disable", nil)
	for i := 0; i < 3; i++ {
		request(s, context.Background(), "GET", "status", nil)
	}
	if f.metadata.Load() != before || s.enabled || len(s.trusted) != 0 {
		t.Fatal("disable did not isolate and forget Exo")
	}
}
func TestAdmissionRejectsChangedIdentityMemoryAndUnknownNodes(t *testing.T) {
	snapshot := Snapshot{Gateway: "g", Nodes: []Node{{ID: "n"}}, Models: []Model{{ID: "m", Tasks: []string{"TextGeneration"}}}, Instances: []Instance{{Model: "m", Nodes: []string{"n"}, ResidentBytes: 1024}}}
	policy := DefaultPolicy()
	input := Completion{Model: "m"}
	trusted := map[string]bool{"n": true}
	if err := admission(snapshot, "g", trusted, input, policy); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*Snapshot)
	}{
		{"gateway", func(s *Snapshot) { s.Gateway = "changed" }},
		{"new node", func(s *Snapshot) { s.Nodes = append(s.Nodes, Node{ID: "new"}) }},
		{"unsupported platform", func(s *Snapshot) { s.Nodes = []Node{{ID: "n", OS: "Windows 11", Backends: []string{"MlxCpu"}}} }},
		{"placement", func(s *Snapshot) {
			s.Instances = []Instance{{Model: "m", Nodes: []string{"unseen"}, ResidentBytes: 1024}}
		}},
		{"unknown memory", func(s *Snapshot) { s.Instances = []Instance{{Model: "m", Nodes: []string{"n"}}} }},
		{"memory budget", func(s *Snapshot) {
			s.Instances = []Instance{{Model: "m", Nodes: []string{"n"}, ResidentBytes: 1 << 50}}
		}},
		{"missing model", func(s *Snapshot) { s.Models = nil }},
		{"unsupported model task", func(s *Snapshot) { s.Models = []Model{{ID: "m", Tasks: []string{"TextToImage"}}} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			copy := snapshot
			c.change(&copy)
			if admission(copy, "g", trusted, input, policy) == nil {
				t.Fatal("unsafe admission accepted")
			}
		})
	}
}
func TestCancellationQueueBoundAndDisable(t *testing.T) {
	started := make(chan struct{}, 2)
	cancelled := make(chan struct{}, 2)
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[]}\n\n")
		w.(http.Flusher).Flush()
		started <- struct{}{}
		<-r.Context().Done()
		cancelled <- struct{}{}
	})
	s := New()
	policy := DefaultPolicy()
	policy.Queued = 1
	connectFixture(t, s, f, policy)
	approve(t, s)
	done := make(chan *httptest.ResponseRecorder, 2)
	go func() { done <- request(s, context.Background(), "POST", "chat", chatInput()) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("stream never started")
	}
	go func() { done <- request(s, context.Background(), "POST", "chat", chatInput()) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		n := s.outstanding()
		s.mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("request was not queued")
		}
		time.Sleep(time.Millisecond)
	}
	if w := request(s, context.Background(), "POST", "chat", chatInput()); w.Code != 429 {
		t.Fatalf("queue overflow: %d", w.Code)
	}
	request(s, context.Background(), "POST", "disable", nil)
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("disable left an active/queued request")
		}
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream request was not cancelled")
	}
	if f.chats.Load() != 1 {
		t.Fatal("queued request reached inference after disable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.State != "cancelled" {
			t.Fatalf("job after disable: %+v", j)
		}
	}
}
func TestDisconnectAndUpstreamFailure(t *testing.T) {
	t.Run("client cancellation", func(t *testing.T) {
		started := make(chan struct{})
		cancelled := make(chan struct{})
		f := newFixture(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(cancelled) })
		s := New()
		connectFixture(t, s, f, DefaultPolicy())
		approve(t, s)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() { request(s, ctx, "POST", "chat", chatInput()); close(done) }()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("request did not reach provider")
		}
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("client disconnect did not release job")
		}
		select {
		case <-cancelled:
		case <-time.After(3 * time.Second):
			t.Fatal("client disconnect did not cancel Exo")
		}
	})
	t.Run("provider failure is not success or prompt logging", func(t *testing.T) {
		f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "private prompt fixture: model failed", 503)
		})
		s := New()
		connectFixture(t, s, f, DefaultPolicy())
		approve(t, s)
		w := request(s, context.Background(), "POST", "chat", chatInput())
		if w.Code != 502 || !strings.Contains(w.Body.String(), "503") {
			t.Fatalf("provider error: %d %s", w.Code, w.Body.String())
		}
		status := request(s, context.Background(), "GET", "status", nil)
		if strings.Contains(status.Body.String(), "private prompt") {
			t.Fatal("prompt leaked into activity")
		}
		if s.jobs[0].State != "failed" {
			t.Fatal("provider error recorded as success")
		}
	})
}

func TestTruncatedStreamAndRequestDeadline(t *testing.T) {
	t.Run("truncated stream", func(t *testing.T) {
		f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[]}\n\n")
		})
		s := New()
		connectFixture(t, s, f, DefaultPolicy())
		approve(t, s)
		w := request(s, context.Background(), "POST", "chat", chatInput())
		if !strings.Contains(w.Body.String(), "disconnected") || s.jobs[0].State != "failed" {
			t.Fatal("truncated stream was reported as successful")
		}
	})
	t.Run("deadline releases upstream and admission", func(t *testing.T) {
		f := newFixture(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		s := New()
		connectFixture(t, s, f, DefaultPolicy())
		approve(t, s)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		request(s, ctx, "POST", "chat", chatInput())
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.outstanding() != 0 || len(s.active) != 0 {
			t.Fatal("request deadline leaked queue or active resources")
		}
	})
}
