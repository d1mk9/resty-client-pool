package restypool

import (
	"context"
	"restyclientpool/pkg/config"
	"sync"

	resty "resty.dev/v3"
)

type Client interface {
	Get(ctx context.Context, path string) (*resty.Response, error)
	Post(ctx context.Context, path string, body any) (*resty.Response, error)
	Close() error
}

type ClientPool struct {
	clients   []*resty.Client
	spin      rr
	cfg       config.Config
	closeOnce sync.Once
}

func New(cfg config.Config) *ClientPool {
	if cfg.Size <= 0 {
		cfg.Size = config.DefaultConfig().Size
	}

	cs := make([]*resty.Client, 0, cfg.Size)
	for i := 0; i < cfg.Size; i++ {
		cs = append(cs, newRestyClient(cfg))
	}
	return &ClientPool{clients: cs, cfg: cfg}
}

func (p *ClientPool) Get(ctx context.Context, path string) (*resty.Response, error) {
	i := p.spin.next(len(p.clients))
	return p.clients[i].R().SetContext(ctx).Get(path)
}

func (p *ClientPool) Post(ctx context.Context, path string, body any) (*resty.Response, error) {
	i := p.spin.next(len(p.clients))
	return p.clients[i].R().SetContext(ctx).SetBody(body).Post(path)
}

func (p *ClientPool) Close() {
	p.closeOnce.Do(func() {
		for _, c := range p.clients {
			_ = c.Close()
		}
	})
}
