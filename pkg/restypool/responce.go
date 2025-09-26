package restypool

import (
	"resty.dev/v3"
)

type restyResp struct {
	status int
	body   []byte
}

func newRestyResp(r *resty.Response) restyResp {
	b := append([]byte(nil), r.Bytes()...)
	return restyResp{
		status: r.StatusCode(),
		body:   b,
	}
}

func (r restyResp) StatusCode() int { return r.status }
func (r restyResp) Body() []byte    { return r.body }
