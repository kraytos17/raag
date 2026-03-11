package tracker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/p-society/raag/internal/logger"
)

type Tracker struct {
	mu    sync.RWMutex
	peers map[string]time.Time // multiaddress -> last seen
}

func (t *Tracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger.Infof("tracker request method=%s path=%s", r.Method, r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/peers":
		t.handleGetPeers(w)
	case "/register":
		t.handleRegisterPeer(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (t *Tracker) handleGetPeers(w http.ResponseWriter) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var peerList []string
	for peer := range t.peers {
		peerList = append(peerList, peer)
	}
	json.NewEncoder(w).Encode(peerList)
}

func (t *Tracker) handleRegisterPeer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var data struct {
		Addr string `json:"addr"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	t.mu.Lock()
	t.peers[data.Addr] = time.Now()
	t.mu.Unlock()

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (t *Tracker) cleanupOldPeers() {
	for {
		time.Sleep(5 * time.Minute)
		t.mu.Lock()
		for peer, lastSeen := range t.peers {
			if time.Since(lastSeen) > 10*time.Minute {
				delete(t.peers, peer)
			}
		}
		t.mu.Unlock()
	}
}

// StartServer starts the tracker HTTP server
func StartServer(port int) {
	tracker := &Tracker{peers: make(map[string]time.Time)}
	go tracker.cleanupOldPeers()

	logger.Infof("starting tracker port=%d", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), tracker); err != nil {
		logger.Errorf("tracker server failed error=%v", err)
		os.Exit(1)
	}
}
