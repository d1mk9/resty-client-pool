package pool_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"httpclientpool/pkg/config"
	"httpclientpool/pkg/fiberpool"
	"httpclientpool/pkg/pool"
	"httpclientpool/pkg/restypool"
)

func newH1TLSServer(latency time.Duration, payload any) *httptest.Server {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if latency > 0 {
			time.Sleep(latency)
		}
		switch r.URL.Path {
		case "/ping":
			_ = json.NewEncoder(w).Encode(payload)
		case "/large":
			blob := bytes.Repeat([]byte("A"), 256*1024)
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool   `json:"ok"`
				Data string `json:"data"`
			}{OK: true, Data: string(blob)})
		default:
			http.NotFound(w, r)
		}
	})
	s := httptest.NewUnstartedServer(h)
	s.Config.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){}
	s.StartTLS()
	return s
}

func cfgFor(url string) config.Config {
	cfg := config.DefaultConfig()
	cfg.BaseURL = url
	cfg.InsecureSkipVerify = true
	cfg.Size = 8
	cfg.MaxConnsPerHost = 1
	cfg.RequestTimeout = 5 * time.Second
	cfg.DialTimeout = 2 * time.Second
	cfg.TlsTimeout = 2 * time.Second
	return cfg
}

func benchClient(b *testing.B, name string, mk func() pool.Client, path string, par int) {
	b.Helper()
	cl := mk()
	defer cl.Close()

	ctx := context.Background()

	for i := 0; i < par*2; i++ {
		_, _ = cl.Get(ctx, path)
	}

	b.ReportAllocs()
	b.SetParallelism(par)
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := cl.Get(ctx, path)
			if err != nil || resp.StatusCode() != 200 {
				b.Fatalf("%s GET %s err=%v status=%v", name, path, err, func() int {
					if resp == nil {
						return 0
					}
					return resp.StatusCode()
				}())
			}
			_ = len(resp.Body())
		}
	})
}

func BenchmarkPools_Small(b *testing.B) {
	srv := newH1TLSServer(200*time.Microsecond, map[string]any{"ok": true})
	defer srv.Close()

	cfg := cfgFor(srv.URL)
	par := cfg.Size

	b.Run("resty/small", func(b *testing.B) {
		benchClient(b, "resty", func() pool.Client {
			return restypool.New(cfg)
		}, "/ping", par)
	})

	b.Run("fiber/small", func(b *testing.B) {
		benchClient(b, "fiber", func() pool.Client {
			return fiberpool.New(cfg)
		}, "/ping", par)
	})
}

func BenchmarkPools_Large(b *testing.B) {
	srv := newH1TLSServer(2*time.Millisecond, map[string]any{"ok": true})
	defer srv.Close()

	cfg := cfgFor(srv.URL)
	par := cfg.Size

	b.Run("resty/large", func(b *testing.B) {
		benchClient(b, "resty", func() pool.Client {
			return restypool.New(cfg)
		}, "/large", par)
	})

	b.Run("fiber/large", func(b *testing.B) {
		benchClient(b, "fiber", func() pool.Client {
			return fiberpool.New(cfg)
		}, "/large", par)
	})
}
