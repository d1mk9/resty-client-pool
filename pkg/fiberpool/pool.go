package fiberpool

import (
	"context"
	"httpclientpool/pkg/config"
	"httpclientpool/pkg/pool"
	"httpclientpool/pkg/rr"
	"sync"

	fibercli "github.com/gofiber/fiber/v3/client"
)

var _ pool.Client = (*ClientPool)(nil)

type ClientPool struct {
	clients   []*fibercli.Client
	spin      rr.RR
	cfg       config.Config
	closeOnce sync.Once
}

func New(cfg config.Config) *ClientPool {
	if cfg.Size <= 0 {
		cfg.Size = config.DefaultConfig().Size
	}
	cs := make([]*fibercli.Client, 0, cfg.Size)
	for i := 0; i < cfg.Size; i++ {
		cs = append(cs, newFiberClient(cfg))
	}
	return &ClientPool{clients: cs, cfg: cfg}
}

func (p *ClientPool) Get(ctx context.Context, path string) (pool.Response, error) {
	i := p.spin.Next(len(p.clients))
	res, err := p.clients[i].Get(path)
	if err != nil {
		return nil, err
	}
	return newFiberResp(res), nil
}

func (p *ClientPool) Post(ctx context.Context, path string, body any) (pool.Response, error) {
	i := p.spin.Next(len(p.clients))
	res, err := p.clients[i].Post(path, fibercli.Config{
		Body: body,
	})
	if err != nil {
		return nil, err
	}
	return newFiberResp(res), nil
}

func (p *ClientPool) Close() {
	p.closeOnce.Do(func() {
	})
}
