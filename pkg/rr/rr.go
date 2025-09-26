package rr

import "sync/atomic"

type RR struct{ n atomic.Uint64 }

func (r *RR) Next(mod int) int {
	x := r.n.Add(1)
	return int((x - 1) % uint64(mod))
}
