package localai

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Policy struct {
	Concurrent     int   `json:"concurrent"`
	Queued         int   `json:"queued"`
	TimeoutSeconds int   `json:"timeoutSeconds"`
	MemoryBudgetMB int64 `json:"memoryBudgetMB"`
	MaxTokens      int   `json:"maxTokens"`
}

func DefaultPolicy() Policy { return Policy{1, 2, 120, 8192, 512} }

// LoopbackListener keeps the integration absent from public/cloud listeners.
func LoopbackListener(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	return err == nil && (host == "localhost" || net.ParseIP(host).IsLoopback())
}
func (p Policy) valid() bool {
	return p.Concurrent >= 1 && p.Concurrent <= 2 && p.Queued >= 0 && p.Queued <= 4 && p.TimeoutSeconds >= 10 && p.TimeoutSeconds <= 300 && p.MemoryBudgetMB >= 128 && p.MemoryBudgetMB <= 1048576 && p.MaxTokens >= 16 && p.MaxTokens <= 2048
}

type Job struct {
	ID       string     `json:"id"`
	Model    string     `json:"model"`
	State    string     `json:"state"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
	Error    string     `json:"error,omitempty"`
	cancel   context.CancelFunc
}
type Service struct {
	mu       sync.Mutex
	token    string
	enabled  bool
	endpoint string
	gateway  string
	provider Provider
	policy   Policy
	trusted  map[string]bool
	jobs     []*Job
	root     context.Context
	stop     context.CancelFunc
	active   chan struct{}
	revision uint64
}

// New creates no process, timer, polling loop or worker goroutine.
func New() *Service {
	return &Service{token: rand.Text(), endpoint: "http://127.0.0.1:52415", policy: DefaultPolicy(), trusted: map[string]bool{}, jobs: []*Job{}}
}
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop != nil {
		s.stop()
	}
	if s.provider != nil {
		s.provider.Close()
	}
	s.revision++
	s.enabled = false
	s.provider = nil
	s.trusted = map[string]bool{}
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, value any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("application/json required")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return errors.New("invalid or oversized AI request")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("only one JSON request is allowed")
	}
	return nil
}
func localRequest(r *http.Request) bool {
	host := r.Host
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return false
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(peer).IsLoopback() {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != scheme+"://"+r.Host {
		return false
	}
	site := r.Header.Get("Sec-Fetch-Site")
	return site == "" || site == "none" || site == "same-origin"
}

func (s *Service) view(includeToken bool) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := map[string]any{"enabled": s.enabled, "endpoint": s.endpoint, "policy": s.policy, "trusted": s.trusted, "jobs": s.jobs, "hostOS": runtime.GOOS, "hostArch": runtime.GOARCH, "exoCommit": ExoCommit}
	// Encode under the lock: jobs and trust are mutable, so hand out copies.
	data, _ := json.Marshal(v)
	var copied map[string]any
	_ = json.Unmarshal(data, &copied)
	if includeToken {
		copied["token"] = s.token
	}
	return copied
}

func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !localRequest(r) {
			fail(w, 403, errors.New("Local AI requires a same-origin loopback connection"))
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/local-ai/")
		if path == "bootstrap" && r.Method == "GET" {
			writeJSON(w, 200, s.view(true))
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-LocalFirst-AI-Token")), []byte(s.token)) != 1 {
			fail(w, 403, errors.New("local AI token required"))
			return
		}
		if path == "status" && r.Method == "GET" {
			writeJSON(w, 200, s.view(false))
			return
		}
		if path == "snapshot" && r.Method == "GET" {
			s.mu.Lock()
			p, enabled, root := s.provider, s.enabled, s.root
			s.mu.Unlock()
			if !enabled {
				fail(w, 409, errors.New("Local AI is disabled"))
				return
			}
			ctx, cancel := context.WithTimeout(root, 8*time.Second)
			defer cancel()
			stop := context.AfterFunc(r.Context(), cancel)
			defer stop()
			snapshot, err := p.Snapshot(ctx)
			if err != nil {
				fail(w, 503, err)
				return
			}
			s.mu.Lock()
			gateway := s.gateway
			s.mu.Unlock()
			if snapshot.Gateway != gateway {
				fail(w, 409, errors.New("Exo gateway identity changed; disable AI and probe again"))
				return
			}
			writeJSON(w, 200, snapshot)
			return
		}
		if r.Method != "POST" {
			fail(w, 405, errors.New("method not allowed"))
			return
		}
		switch path {
		case "probe", "connect":
			s.mu.Lock()
			revision := s.revision
			s.mu.Unlock()
			var body struct {
				Endpoint string `json:"endpoint"`
				Gateway  string `json:"gateway"`
				Consent  bool   `json:"consent"`
				Policy   Policy `json:"policy"`
			}
			if err := decode(w, r, &body); err != nil {
				fail(w, 400, err)
				return
			}
			p, err := NewExo(body.Endpoint)
			if err != nil {
				fail(w, 400, err)
				return
			}
			retained := false
			defer func() {
				if !retained {
					p.Close()
				}
			}()
			snapshot, err := p.Snapshot(r.Context())
			if err != nil {
				fail(w, 503, err)
				return
			}
			if path == "probe" {
				writeJSON(w, 200, snapshot)
				return
			}
			if !body.Consent || body.Gateway != snapshot.Gateway || !body.Policy.valid() {
				fail(w, 400, errors.New("approve the probed gateway and provide a valid resource policy"))
				return
			}
			s.mu.Lock()
			if s.enabled || s.outstanding() > 0 || revision != s.revision {
				s.mu.Unlock()
				fail(w, 409, errors.New("disable AI and wait for cancelled jobs before reconnecting"))
				return
			}
			s.enabled, s.endpoint, s.provider, s.policy = true, body.Endpoint, p, body.Policy
			s.gateway = snapshot.Gateway
			s.trusted = map[string]bool{}
			s.root, s.stop = context.WithCancel(context.Background())
			s.active = make(chan struct{}, body.Policy.Concurrent)
			retained = true
			s.mu.Unlock()
			writeJSON(w, 200, s.view(false))
		case "disable":
			s.Close()
			writeJSON(w, 200, s.view(false))
		case "trust":
			var body struct {
				Node    string `json:"node"`
				Trusted bool   `json:"trusted"`
			}
			if err := decode(w, r, &body); err != nil {
				fail(w, 400, err)
				return
			}
			s.mu.Lock()
			p, root, gateway, revision, enabled := s.provider, s.root, s.gateway, s.revision, s.enabled
			s.mu.Unlock()
			if !enabled {
				fail(w, 409, errors.New("Local AI is disabled"))
				return
			}
			if body.Trusted {
				ctx, cancel := context.WithTimeout(root, 8*time.Second)
				defer cancel()
				stop := context.AfterFunc(r.Context(), cancel)
				defer stop()
				snapshot, err := p.Snapshot(ctx)
				if err != nil {
					fail(w, 503, err)
					return
				}
				found := false
				for _, node := range snapshot.Nodes {
					if node.ID == body.Node {
						found = true
					}
				}
				if snapshot.Gateway != gateway || !found {
					fail(w, 409, errors.New("approve only a currently reported node from the approved gateway"))
					return
				}
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if !s.enabled || revision != s.revision || body.Node == "" || len(body.Node) > 256 {
				fail(w, 400, errors.New("enable AI and specify a node ID"))
				return
			}
			s.revision++
			// Revocation also stops queued/active work. Approval never starts it.
			if !body.Trusted {
				s.stop()
				s.root, s.stop = context.WithCancel(context.Background())
				delete(s.trusted, body.Node)
			} else {
				s.trusted[body.Node] = true
			}
			writeJSON(w, 200, map[string]bool{"trusted": body.Trusted})
		case "cancel":
			var body struct {
				ID string `json:"id"`
			}
			if err := decode(w, r, &body); err != nil {
				fail(w, 400, err)
				return
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			for _, job := range s.jobs {
				if job.ID == body.ID && job.Finished == nil {
					job.cancel()
					writeJSON(w, 200, map[string]bool{"cancelled": true})
					return
				}
			}
			fail(w, 404, errors.New("active job not found"))
		case "chat":
			s.chat(w, r)
		default:
			fail(w, 404, errors.New("unknown local AI operation"))
		}
	})
}

// outstanding is called only while holding mu. The queue uses waiting HTTP
// requests; it has a strict admission bound and no persistent worker goroutines.
func (s *Service) outstanding() int {
	n := 0
	for _, j := range s.jobs {
		if j.Finished == nil {
			n++
		}
	}
	return n
}
func (s *Service) finish(job *Job, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	job.Finished = &now
	job.State = "completed"
	if err != nil {
		job.State, job.Error = "failed", "Inference failed; inspect the request error or Exo service logs."
		if errors.Is(err, context.Canceled) {
			job.State, job.Error = "cancelled", "Request cancelled"
		}
		if errors.Is(err, context.DeadlineExceeded) {
			job.State, job.Error = "failed", "Inference time limit exceeded"
		}
	}
	job.cancel()
}

func admission(snapshot Snapshot, gateway string, trusted map[string]bool, input Completion, policy Policy) error {
	if snapshot.Gateway != gateway {
		return errors.New("Exo gateway identity changed; disable and probe it again")
	}
	if len(snapshot.Nodes) == 0 {
		return errors.New("Exo has no reported nodes")
	}
	known := map[string]bool{}
	for _, n := range snapshot.Nodes {
		known[n.ID] = true
		os := strings.ToLower(n.OS)
		if len(n.Backends) > 0 && (strings.Contains(os, "windows") || strings.Contains(os, "android")) {
			return fmt.Errorf("Exo compute on node %s reports an unsupported platform", n.ID)
		}
		if !trusted[n.ID] {
			return fmt.Errorf("approve Exo node %s before sending any prompt", n.ID)
		}
		if n.LastSeen != nil && time.Since(*n.LastSeen) > time.Minute {
			return fmt.Errorf("Exo node %s is disconnected or stale", n.ID)
		}
	}
	downloaded := false
	for _, m := range snapshot.Models {
		if m.ID == input.Model {
			downloaded = true
		}
	}
	if !downloaded {
		return errors.New("select a downloaded model; this module never downloads weights")
	}
	found := false
	for _, instance := range snapshot.Instances {
		if instance.Model != input.Model {
			continue
		}
		found = true
		if len(instance.Nodes) == 0 {
			return errors.New("Exo model has no verified compute placement")
		}
		for _, node := range instance.Nodes {
			if !known[node] || !trusted[node] {
				return errors.New("model placement contains an unapproved node")
			}
		}
		if instance.ResidentBytes <= 0 {
			return errors.New("model memory requirement is unknown; cannot apply the configured admission budget")
		}
		if instance.ResidentBytes > policy.MemoryBudgetMB*(1<<20) {
			return errors.New("model resident estimate exceeds the configured advisory memory budget")
		}
	}
	if !found {
		return errors.New("load this model in Exo first; no active instance was reported")
	}
	return nil
}

func (s *Service) chat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Completion
		Consent bool `json:"consent"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, 400, err)
		return
	}
	if !body.Consent || body.Model == "" || len(body.Model) > 256 || len(body.Messages) == 0 || len(body.Messages) > 32 {
		fail(w, 400, errors.New("explicit prompt consent, a model and 1-32 messages required"))
		return
	}
	total := 0
	for _, m := range body.Messages {
		if (m.Role != "user" && m.Role != "assistant" && m.Role != "system") || len(m.Content) > 8192 {
			fail(w, 400, errors.New("invalid message role or message over 8 KiB"))
			return
		}
		total += len(m.Content)
	}
	if total == 0 || total > 16<<10 {
		fail(w, 400, errors.New("prompt must contain 1-16384 bytes"))
		return
	}
	s.mu.Lock()
	if !s.enabled {
		s.mu.Unlock()
		fail(w, 409, errors.New("Local AI is disabled"))
		return
	}
	policy := s.policy
	if body.MaxTokens < 1 || body.MaxTokens > policy.MaxTokens {
		s.mu.Unlock()
		fail(w, 400, errors.New("requested output exceeds the configured token limit"))
		return
	}
	if s.outstanding() >= policy.Concurrent+policy.Queued {
		s.mu.Unlock()
		fail(w, 429, errors.New("AI queue is full; document collaboration remains independent"))
		return
	}
	ctx, cancel := context.WithTimeout(s.root, time.Duration(policy.TimeoutSeconds)*time.Second)
	stopDisconnect := context.AfterFunc(r.Context(), cancel)
	defer stopDisconnect()
	defer cancel()
	job := &Job{ID: rand.Text(), Model: body.Model, State: "queued", Started: time.Now().UTC(), cancel: cancel}
	if len(s.jobs) >= 64 {
		for i, j := range s.jobs {
			if j.Finished != nil {
				s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
				break
			}
		}
	}
	s.jobs = append(s.jobs, job)
	p, active, gateway := s.provider, s.active, s.gateway
	s.mu.Unlock()
	var result error
	defer func() { s.finish(job, result) }()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Duration(policy.TimeoutSeconds+10) * time.Second))
	select {
	case active <- struct{}{}:
		defer func() { <-active }()
	case <-ctx.Done():
		result = ctx.Err()
		fail(w, 408, result)
		return
	}
	s.mu.Lock()
	job.State = "running"
	s.mu.Unlock()
	snapshot, err := p.Snapshot(ctx)
	if err != nil {
		result = err
		fail(w, 503, err)
		return
	}
	s.mu.Lock()
	err = admission(snapshot, gateway, s.trusted, body.Completion, policy)
	s.mu.Unlock()
	if err != nil {
		result = err
		fail(w, 409, err)
		return
	}
	res, err := p.Complete(ctx, body.Completion)
	if err != nil {
		result = err
		if ctx.Err() != nil {
			result = ctx.Err()
		}
		fail(w, 503, result)
		return
	}
	defer res.Body.Close() // Client disconnect/disable also cancels the upstream HTTP request.
	if res.StatusCode != 200 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		result = fmt.Errorf("Exo returned HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(data)))
		fail(w, 502, result)
		return
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		result = errors.New("Exo did not return an SSE response")
		fail(w, 502, result)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-LocalFirst-AI-Job", job.ID)
	controller := http.NewResponseController(w)
	scanner := bufio.NewScanner(io.LimitReader(res.Body, (2<<20)+1))
	scanner.Buffer(make([]byte, 4096), 128<<10)
	size, done := 0, false
	for scanner.Scan() {
		line := scanner.Text()
		size += len(line) + 1
		if size > 2<<20 {
			result = errors.New("AI stream exceeded 2 MiB")
			break
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				done = true
			} else {
				var event struct {
					Error json.RawMessage `json:"error"`
				}
				if json.Unmarshal([]byte(data), &event) == nil && len(event.Error) > 0 && string(event.Error) != "null" {
					result = errors.New("Exo reported an inference error; see the response")
				}
			}
		}
		_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := fmt.Fprintln(w, line); err != nil {
			result = err
			return
		}
		if line == "" {
			if err := controller.Flush(); err != nil {
				result = err
				return
			}
		}
	}
	if ctx.Err() != nil {
		result = ctx.Err()
	} else if scanner.Err() != nil {
		result = scanner.Err()
	} else if !done && result == nil {
		result = errors.New("Exo disconnected before completing the response")
	}
	if result != nil {
		data, _ := json.Marshal(map[string]any{"error": map[string]string{"message": result.Error()}})
		_, _ = fmt.Fprintf(w, "\ndata: %s\n\ndata: [DONE]\n\n", data)
		_ = controller.Flush()
	}
}
