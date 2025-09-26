package fiberpool

import (
	"crypto/tls"
	"net"

	"httpclientpool/pkg/config"

	fibercli "github.com/gofiber/fiber/v3/client"
	"github.com/valyala/fasthttp"
)

func newFiberBase(cfg config.Config) *fasthttp.Client {
	return &fasthttp.Client{
		Dial:                func(addr string) (net.Conn, error) { return fasthttp.DialTimeout(addr, cfg.DialTimeout) },
		TLSConfig:           &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
		ReadTimeout:         cfg.RequestTimeout,
		WriteTimeout:        cfg.RequestTimeout,
		MaxIdleConnDuration: cfg.IdleConnTimeout,
		MaxConnsPerHost:     cfg.MaxConnsPerHost,
		MaxConnWaitTimeout:  cfg.RequestTimeout,
	}
}

func newFiberClient(cfg config.Config) *fibercli.Client {
	return fibercli.NewWithClient(newFiberBase(cfg)).SetTimeout(cfg.RequestTimeout).SetBaseURL(cfg.BaseURL)
}
