package restypool

import (
	"crypto/tls"
	"net"
	"net/http"
	"restyclientpool/pkg/config"

	resty "resty.dev/v3"
)

func newHTTPTransport(cfg config.Config) *http.Transport {
	tlsCfg := &tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}

	tr := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: cfg.DialTimeout}).DialContext,
		TLSClientConfig:     tlsCfg,
		TLSHandshakeTimeout: cfg.TlsTimeout,
		IdleConnTimeout:     cfg.IdleConnTimeout,

		MaxConnsPerHost:       cfg.MaxConnsPerHost,
		MaxIdleConnsPerHost:   cfg.MaxConnsPerHost,
		MaxIdleConns:          cfg.Size * 2,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	return tr
}

func newRestyClient(cfg config.Config) *resty.Client {
	return resty.New().
		SetTimeout(cfg.RequestTimeout).
		SetTransport(newHTTPTransport(cfg)).
		SetBaseURL(cfg.BaseURL)
}
