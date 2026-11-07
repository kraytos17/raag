package protocols

import (
	"sync"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestAdmissionRegistry_Lifecycle(t *testing.T) {
	r := NewAdmissionRegistry()
	pid := peer.ID("lifecycle-peer")

	if r.IsAdmitted(pid) {
		t.Fatal("IsAdmitted() = true for unknown peer, want false")
	}

	r.Admit(pid)
	if !r.IsAdmitted(pid) {
		t.Fatal("IsAdmitted() = false after Admit, want true")
	}

	// Revoking an unknown peer is a no-op, not a panic.
	r.Revoke(peer.ID("unknown"))

	r.Revoke(pid)
	if r.IsAdmitted(pid) {
		t.Fatal("IsAdmitted() = true after Revoke, want false")
	}
}

func TestAdmissionRegistry_ConcurrentUse(t *testing.T) {
	r := NewAdmissionRegistry()
	pids := []peer.ID{peer.ID("a"), peer.ID("b"), peer.ID("c")}

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Go(func() {
			pid := pids[i%len(pids)]
			r.Admit(pid)
			_ = r.IsAdmitted(pid)
			if i%2 == 0 {
				r.Revoke(pid)
			}
		})
	}
	wg.Wait()
}
