package config

import "time"

type Config struct {
	BaseURL               string
	Size                  int
	RequestTimeout        time.Duration
	DialTimeout           time.Duration
	TlsTimeout            time.Duration
	IdleConnTimeout       time.Duration
	MaxConnsPerHost       int
	InsecureSkipVerify    bool
	ResponseHeaderTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		BaseURL:               "",
		Size:                  8,
		RequestTimeout:        10 * time.Second,
		DialTimeout:           5 * time.Second,
		TlsTimeout:            2 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxConnsPerHost:       1,
		InsecureSkipVerify:    true,
		ResponseHeaderTimeout: 0,
	}
}
