package server

import (
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

type SubscriptionManager struct {
	mu   sync.RWMutex
	subs map[net.Conn]*connState
}

// connState is the per-connection record. It owns the single write mutex for
// the connection so response writes, scan-progress broadcasts, and event
// broadcasts all serialize on the same lock (one owner for "who may write to
// this conn right now").
type connState struct {
	conn      net.Conn
	eventMask uint32 // 0 until a Subscribe request sets it
	writeMu   sync.Mutex
}

func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		subs: make(map[net.Conn]*connState),
	}
}

// Register adds a newly accepted connection to the manager. Call once in the
// accept loop; the conn now owns its write mutex even before subscribing.
func (sm *SubscriptionManager) Register(conn net.Conn) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.subs[conn] = &connState{conn: conn}
}

// Subscribe sets the event mask on an existing connection's state. The
// connection must already be registered.
func (sm *SubscriptionManager) Subscribe(conn net.Conn, eventMask uint32) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if st, ok := sm.subs[conn]; ok {
		st.eventMask = eventMask
		return
	}
	sm.subs[conn] = &connState{conn: conn, eventMask: eventMask}
}

// Unsubscribe removes a connection.
func (sm *SubscriptionManager) Unsubscribe(conn net.Conn) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.subs, conn)
}

// WriteTo serializes a message write to a connection under the connection's
// single write mutex. All response, scan-progress, and event writes go
// through this so they can never interleave frames.
func (sm *SubscriptionManager) WriteTo(conn net.Conn, msg *pb.Response) error {
	sm.mu.RLock()
	st := sm.subs[conn]
	sm.mu.RUnlock()
	if st == nil {
		// Unregistered conn: write without a shared owner (defensive; the
		// accept loop registers every conn before it is used).
		return wire.WriteMsg(conn, msg)
	}

	st.writeMu.Lock()
	defer st.writeMu.Unlock()
	return wire.WriteMsg(conn, msg)
}

// All returns a snapshot of all registered connections.
func (sm *SubscriptionManager) All() []*connState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	out := make([]*connState, 0, len(sm.subs))
	for _, st := range sm.subs {
		out = append(out, st)
	}
	return out
}

// Broadcast sends an event to all subscribers who are interested in this event type
func (sm *SubscriptionManager) Broadcast(eventType uint32, payload []byte) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	event := &pb.Event{
		EventType: pb.EventType(eventType),
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	for _, st := range sm.subs {
		if st.eventMask&eventType == 0 {
			continue // Not subscribed to this event type
		}

		st.writeMu.Lock()
		err := wire.WriteMsg(st.conn, event)
		st.writeMu.Unlock()
		if err != nil {
			slog.Debug("event broadcast failed", "error", err)
		}
	}
}

// Count returns the number of active connections
func (sm *SubscriptionManager) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.subs)
}
