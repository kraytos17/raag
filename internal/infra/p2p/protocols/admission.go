package protocols

import (
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
)

// AdmissionRegistry tracks which peers have completed manifest exchange
// and are therefore authorized to access protected resources like chunks.
type AdmissionRegistry struct {
	mu       sync.RWMutex
	admitted map[peer.ID]struct{}
}

func NewAdmissionRegistry() *AdmissionRegistry {
	return &AdmissionRegistry{
		admitted: make(map[peer.ID]struct{}),
	}
}

func (r *AdmissionRegistry) Admit(pid peer.ID) {
	r.mu.Lock()
	r.admitted[pid] = struct{}{}
	r.mu.Unlock()
}

func (r *AdmissionRegistry) IsAdmitted(pid peer.ID) bool {
	r.mu.RLock()
	ok := false
	_, ok = r.admitted[pid]
	r.mu.RUnlock()
	return ok
}

func (r *AdmissionRegistry) Revoke(pid peer.ID) {
	r.mu.Lock()
	delete(r.admitted, pid)
	r.mu.Unlock()
}
