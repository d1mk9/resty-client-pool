package restypool

import (
	"crypto/tls"
	"net"
	"net/http"
	"httpclientpool/pkg/config"

	resty "resty.dev/v3"
)

func newHTTPTransport(cfg config.Config) *http.Transport {
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: cfg.DialTimeout}).DialContext,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
		TLSHandshakeTimeout:   cfg.TlsTimeout,
		IdleConnTimeout:       cfg.IdleConnTimeout,
		MaxConnsPerHost:       cfg.MaxConnsPerHost,
		MaxIdleConnsPerHost:   cfg.MaxConnsPerHost,
		MaxIdleConns:          cfg.Size * 2,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
}

func newRestyClient(cfg config.Config) *resty.Client {
	return resty.New().SetTimeout(cfg.RequestTimeout).SetTransport(newHTTPTransport(cfg)).SetBaseURL(cfg.BaseURL)
}
