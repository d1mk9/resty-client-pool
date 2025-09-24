package restypool

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"restyclientpool/pkg/config"
)

func newH1TLSServerWithHandler(h http.Handler) *httptest.Server {
	srv := httptest.NewUnstartedServer(h)
	srv.Config.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){}
	srv.StartTLS()
	return srv
}

func TestPool_Get_Post(t *testing.T) {
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
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true
	p := New(cfg)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := p.Get(ctx, "/ping")
	if err != nil {
		t.Fatalf("GET /ping error: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("GET status=%d body=%s", resp.StatusCode(), resp.String())
	}

	body := map[string]string{"hello": "world"}
	resp, err = p.Post(ctx, "/echo", body)
	if err != nil {
		t.Fatalf("POST /echo error: %v", err)
	}
	if resp.StatusCode() != 200 || !bytes.Contains(resp.Bytes(), []byte("hello")) {
		t.Fatalf("POST bad response: %s", resp.String())
	}
}

func TestPool_ContextTimeout_Get(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true
	p := New(cfg)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := p.Get(ctx, "/slow")
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if !(errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(err.Error(), "context deadline exceeded")) {
		t.Fatalf("want context timeout, got: %v", err)
	}
}

func TestPool_ContextCancel_Get(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true
	p := New(cfg)
	defer p.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // мгновенно отменяем

	_, err := p.Get(ctx, "/any")
	if err == nil {
		t.Fatalf("expected canceled error, got nil")
	}
	if !(errors.Is(err, context.Canceled) ||
		strings.Contains(err.Error(), "context canceled")) {
		t.Fatalf("want context canceled, got: %v", err)
	}
}

func TestPool_Parallel_NoRace(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true
	cfg.Size = 8
	p := New(cfg)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	const workers = 200
	var wg sync.WaitGroup
	wg.Add(workers)

	var fails atomic.Int64
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			resp, err := p.Get(ctx, "/ok")
			if err != nil || resp.StatusCode() != 200 {
				fails.Add(1)
			}
		}()
	}
	wg.Wait()
	if fails.Load() != 0 {
		t.Fatalf("parallel requests failed: %d", fails.Load())
	}
}

func TestPool_Close_Idempotent(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true
	p := New(cfg)

	p.Close()
	p.Close()
}

func TestPool_BaseURL_Join(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hello" {
			http.Error(w, "bad path: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true
	p := New(cfg)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := p.Get(ctx, "/hello")
	if err != nil {
		t.Fatalf("GET /hello error: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("GET /hello status=%d body=%s", resp.StatusCode(), resp.String())
	}
}

func TestPool_ResponseHeaderTimeout(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true
	cfg.ResponseHeaderTimeout = 50 * time.Millisecond
	p := New(cfg)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := p.Get(ctx, "/slow-headers")
	if err == nil {
		t.Fatalf("expected response header timeout, got nil")
	}
	if !strings.Contains(err.Error(), "timeout awaiting response headers") &&
		!strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("want header timeout, got: %v", err)
	}
}

func TestPool_DefaultSize(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.Size = 0
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true

	p := New(cfg)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := p.Get(ctx, "/x")
	if err != nil || resp.StatusCode() != 200 {
		t.Fatalf("pool with default size failed: err=%v status=%v", err, resp.StatusCode())
	}
}

func TestPool_DistributesAcrossConnections(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]struct{})

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.RemoteAddr] = struct{}{}
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true
	cfg.Size = 8
	p := New(cfg)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

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
		t.Fatalf("expected requests to use multiple TCP connections, got %d", n)
	}
	t.Logf("unique TCP connections: %d", n)
}
