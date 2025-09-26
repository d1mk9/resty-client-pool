package pool_test

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

	"httpclientpool/pkg/config"
	"httpclientpool/pkg/fiberpool"
	"httpclientpool/pkg/pool"
	"httpclientpool/pkg/restypool"
)

func newH1TLSServerWithHandler(h http.Handler) *httptest.Server {
	srv := httptest.NewUnstartedServer(h)
	srv.Config.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){}
	srv.StartTLS()
	return srv
}

type ClientFactory func(cfg config.Config) pool.Client

type SuiteOpts struct {
	HasResponseHeaderTimeout bool
	SupportsContext          bool
	ParallelWorkers          int
}

func Test_ClientPools(t *testing.T) {
	RunClientSuite(t, "resty", func(cfg config.Config) pool.Client {
		return restypool.New(cfg)
	}, SuiteOpts{
		HasResponseHeaderTimeout: true,
		SupportsContext:          true,
		ParallelWorkers:          200,
	})

	RunClientSuite(t, "fiber", func(cfg config.Config) pool.Client {
		return fiberpool.New(cfg)
	}, SuiteOpts{
		HasResponseHeaderTimeout: false, // fasthttp/fiber client это не экспонирует
		SupportsContext:          false, // у fiber нет per-request ctx API
		ParallelWorkers:          64,    // чуть мягче стресс для fasthttp
	})
}

func RunClientSuite(t *testing.T, name string, newClient ClientFactory, opts SuiteOpts) {
	t.Helper()

	t.Run(name+"/GetPost", func(t *testing.T) { testGetPost(t, newClient) })

	if opts.SupportsContext {
		t.Run(name+"/ContextTimeout", func(t *testing.T) { testContextTimeout(t, newClient) })
		t.Run(name+"/ContextCancel", func(t *testing.T) { testContextCancel(t, newClient) })
	} else {
		t.Run(name+"/ContextTimeout", func(t *testing.T) {
			t.Skip("per-request context не поддерживается этой реализацией")
		})
		t.Run(name+"/ContextCancel", func(t *testing.T) {
			t.Skip("per-request context не поддерживается этой реализацией")
		})
	}

	t.Run(name+"/ParallelNoRace", func(t *testing.T) {
		workers := opts.ParallelWorkers
		if workers <= 0 {
			workers = 200
		}
		testParallelNoRace(t, newClient, workers)
	})

	t.Run(name+"/CloseIdempotent", func(t *testing.T) { testCloseIdempotent(t, newClient) })
	t.Run(name+"/BaseURLJoin", func(t *testing.T) { testBaseURLJoin(t, newClient) })
	t.Run(name+"/DefaultSize", func(t *testing.T) { testDefaultSize(t, newClient) })
	t.Run(name+"/DistributesAcrossConnections", func(t *testing.T) {
		testDistributesAcrossConnections(t, newClient)
	})

	if opts.HasResponseHeaderTimeout {
		t.Run(name+"/ResponseHeaderTimeout", func(t *testing.T) {
			testResponseHeaderTimeout(t, newClient)
		})
	} else {
		t.Run(name+"/ResponseHeaderTimeout", func(t *testing.T) {
			t.Skip("ResponseHeaderTimeout не поддерживается этой реализацией")
		})
	}
}

func testGetPost(t *testing.T, newClient ClientFactory) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ping":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/echo":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "echo": m})
		default:
			http.NotFound(w, r)
		}
	})
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true

	p := newClient(cfg)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := p.Get(ctx, "/ping")
	if err != nil {
		t.Fatalf("GET /ping error: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("GET status=%d body=%s", resp.StatusCode(), resp.Body())
	}

	body := map[string]string{"hello": "world"}
	resp, err = p.Post(ctx, "/echo", body)
	if err != nil {
		t.Fatalf("POST /echo error: %v", err)
	}
	if resp.StatusCode() != 200 || !bytes.Contains(resp.Body(), []byte("hello")) {
		t.Fatalf("POST bad response: %s", resp.Body())
	}
}

func testContextTimeout(t *testing.T, newClient ClientFactory) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true

	p := newClient(cfg)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := p.Get(ctx, "/slow")
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if !(errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(err.Error(), "context deadline exceeded") ||
		strings.Contains(err.Error(), "deadline exceeded")) {
		t.Fatalf("want context timeout, got: %v", err)
	}
}

func testContextCancel(t *testing.T, newClient ClientFactory) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true

	p := newClient(cfg)
	defer p.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Get(ctx, "/any")
	if err == nil {
		t.Fatalf("expected canceled error, got nil")
	}
	if !(errors.Is(err, context.Canceled) ||
		strings.Contains(err.Error(), "context canceled")) {
		t.Fatalf("want context canceled, got: %v", err)
	}
}

func testParallelNoRace(t *testing.T, newClient ClientFactory, workers int) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true
	cfg.Size = 8

	p := newClient(cfg)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

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

	if n := fails.Load(); n != 0 {
		t.Fatalf("parallel requests failed: %d", n)
	}
}

func testCloseIdempotent(t *testing.T, newClient ClientFactory) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true

	p := newClient(cfg)

	p.Close()
	p.Close()
}

func testBaseURLJoin(t *testing.T, newClient ClientFactory) {
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

	p := newClient(cfg)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := p.Get(ctx, "/hello")
	if err != nil {
		t.Fatalf("GET /hello error: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("GET /hello status=%d body=%s", resp.StatusCode(), resp.Body())
	}
}

func testDefaultSize(t *testing.T, newClient ClientFactory) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := newH1TLSServerWithHandler(h)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.Size = 0
	cfg.BaseURL = srv.URL
	cfg.InsecureSkipVerify = true

	p := newClient(cfg)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := p.Get(ctx, "/x")
	if err != nil || resp.StatusCode() != 200 {
		t.Fatalf("pool with default size failed: err=%v status=%v", err, resp.StatusCode())
	}
}

func testDistributesAcrossConnections(t *testing.T, newClient ClientFactory) {
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

	p := newClient(cfg)
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

func testResponseHeaderTimeout(t *testing.T, newClient ClientFactory) {
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

	p := newClient(cfg)
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
