package rr_test

import (
	"testing"

	"httpclientpool/pkg/rr"
)

func TestRR_Next_SequenceAndWrap(t *testing.T) {
	var r rr.RR
	mod := 4

	want := []int{0, 1, 2, 3, 0, 1, 2, 3}
	for i, w := range want {
		if got := r.Next(mod); got != w {
			t.Fatalf("step %d: got %d, want %d", i, got, w)
		}
	}
}

func TestRR_Next_ModOne(t *testing.T) {
	var r rr.RR
	for i := 0; i < 10; i++ {
		if got := r.Next(1); got != 0 {
			t.Fatalf("got %d, want 0", got)
		}
	}
}
