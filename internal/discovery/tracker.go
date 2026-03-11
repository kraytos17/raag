package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

type TrackerClient struct {
	trackerURL string
	client     *http.Client
}

func NewTrackerClient(trackerURL string) *TrackerClient {
	return &TrackerClient{
		trackerURL: trackerURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *TrackerClient) FetchPeers(ctx context.Context) ([]peer.AddrInfo, error) {
	if t.trackerURL == "" {
		return nil, fmt.Errorf("no tracker URL configured")
	}

	peersURL := t.trackerURL
	if !strings.HasSuffix(peersURL, "/peers") {
		if strings.HasSuffix(peersURL, "/") {
			peersURL = peersURL + "peers"
		} else {
			peersURL = peersURL + "/peers"
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", peersURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tracker request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tracker returned status %d", resp.StatusCode)
	}

	var multiaddrs []string
	if err := json.NewDecoder(resp.Body).Decode(&multiaddrs); err != nil {
		return nil, fmt.Errorf("failed to decode tracker response: %w", err)
	}

	var peers []peer.AddrInfo
	for _, addrStr := range multiaddrs {
		ma, err := multiaddr.NewMultiaddr(addrStr)
		if err != nil {
			continue
		}

		addrInfo, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			continue
		}
		peers = append(peers, *addrInfo)
	}
	return peers, nil
}

func (t *TrackerClient) RegisterPeer(ctx context.Context, multiaddrStr string) error {
	if t.trackerURL == "" {
		return nil
	}

	trackerURL := t.trackerURL
	var registerURL string
	if strings.HasSuffix(trackerURL, "/peers") {
		registerURL = trackerURL[:len(trackerURL)-len("/peers")] + "/register"
	} else if strings.HasSuffix(trackerURL, "/peers/") {
		registerURL = trackerURL[:len(trackerURL)-len("/peers/")] + "/register"
	} else {
		if trackerURL[len(trackerURL)-1] == '/' {
			registerURL = trackerURL + "register"
		} else {
			registerURL = trackerURL + "/register"
		}
	}

	data := struct {
		Addr string `json:"addr"`
	}{Addr: multiaddrStr}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", registerURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tracker registration failed: %d", resp.StatusCode)
	}
	return nil
}
