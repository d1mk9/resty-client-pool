package fiberpool

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"httpclientpool/pkg/config"
)

func newTLSServer(h http.Handler) *httptest.Server {
	return httptest.NewTLSServer(h)
}

func TestFiberPool_GetPost(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ping":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/echo":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":   true,
				"echo": m,
			})
		default:
			http.NotFound(w, r)
		}
	})
	srv := newTLSServer(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true

	p := New(cfg)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := p.Get(ctx, "/ping")
	if err != nil {
		t.Fatalf("GET /ping error: %v", err)
	}
	if res.StatusCode() != 200 {
		t.Fatalf("GET status=%d body=%s", res.StatusCode(), res.Body())
	}

	body := map[string]string{"hello": "world"}
	res, err = p.Post(ctx, "/echo", body)
	if err != nil {
		t.Fatalf("POST /echo error: %v", err)
	}
	if res.StatusCode() != 200 || !bytes.Contains(res.Body(), []byte("hello")) {
		t.Fatalf("POST bad response: %s", res.Body())
	}
}

func TestFiberPool_ClientTimeout_Get(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newTLSServer(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true
	cfg.RequestTimeout = 50 * time.Millisecond

	p := New(cfg)
	defer p.Close()

	ctx := context.Background()

	_, err := p.Get(ctx, "/slow")
	if err == nil {
		t.Fatalf("expected client timeout error, got nil")
	}
}

func TestFiberPool_Parallel_NoRace(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newTLSServer(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true
	cfg.Size = 8

	p := New(cfg)
	defer p.Close()

	ctx := context.Background()

	const workers = 200
	var wg sync.WaitGroup
	wg.Add(workers)

	var fails atomic.Int64
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			res, err := p.Get(ctx, "/ok")
			if err != nil || res.StatusCode() != 200 {
				fails.Add(1)
			}
		}()
	}
	wg.Wait()
	if fails.Load() != 0 {
		t.Fatalf("parallel requests failed: %d", fails.Load())
	}
}

func TestFiberPool_Close_Idempotent(t *testing.T) {
	cfg := config.DefaultConfig()
	p := New(cfg)
	p.Close()
	p.Close()
}

func TestFiberPool_BaseURL_Join(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hello" {
			http.Error(w, "bad path: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newTLSServer(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true

	p := New(cfg)
	defer p.Close()

	ctx := context.Background()

	res, err := p.Get(ctx, "/hello")
	if err != nil {
		t.Fatalf("GET /hello error: %v", err)
	}
	if res.StatusCode() != 200 {
		t.Fatalf("GET /hello status=%d body=%s", res.StatusCode(), res.Body())
	}
}

func TestFiberPool_DefaultSize(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newTLSServer(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.Size = 0
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true

	p := New(cfg)
	defer p.Close()

	ctx := context.Background()

	res, err := p.Get(ctx, "/x")
	if err != nil || res.StatusCode() != 200 {
		t.Fatalf("pool with default size failed: err=%v status=%v", err, res.StatusCode())
	}
}

func TestFiberPool_DistributesAcrossConnections(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]struct{})

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.RemoteAddr] = struct{}{}
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newTLSServer(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true
	cfg.Size = 8

	p := New(cfg)
	defer p.Close()

	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = p.Get(ctx, "/rr")
		}()
	}
	wg.Wait()

	mu.Lock()
	n := len(seen)
	mu.Unlock()

	if n < 2 {
		t.Fatalf("expected multiple TCP connections, got %d", n)
	}
	t.Logf("unique TCP connections: %d", n)
}
