package restypool

import "sync/atomic"

type rr struct{ n atomic.Uint64 }

func (r *rr) next(mod int) int {
	x := r.n.Add(1)
	return int((x - 1) % uint64(mod))
}
