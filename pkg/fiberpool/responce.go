package fiberpool

import (
	fibercli "github.com/gofiber/fiber/v3/client"
)

type fiberResp struct {
	status int
	body   []byte
}

func newFiberResp(r *fibercli.Response) fiberResp {
	b := append([]byte(nil), r.Body()...)
	return fiberResp{
		status: r.StatusCode(),
		body:   b,
	}
}

func (r fiberResp) StatusCode() int { return r.status }
func (r fiberResp) Body() []byte    { return r.body }
