package pool

import "context"

type Client interface {
	Get(ctx context.Context, path string) (Response, error)
	Post(ctx context.Context, path string, body any) (Response, error)
	Close()
}

type Response interface {
	StatusCode() int
	Body() []byte
}
