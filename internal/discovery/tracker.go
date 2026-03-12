package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	pathpkg "path"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/p-society/raag/internal/constants"
)

type TrackerClient struct {
	trackerURL string
	client     *http.Client
}

func NewTrackerClient(trackerURL string) *TrackerClient {
	return &TrackerClient{
		trackerURL: trackerURL,
		client:     &http.Client{Timeout: constants.HTTPClientTimeout},
	}
}

func (t *TrackerClient) FetchPeers(ctx context.Context) ([]peer.AddrInfo, error) {
	if t.trackerURL == "" {
		return nil, fmt.Errorf("no tracker URL configured")
	}

	peersURL, err := joinTrackerPath(t.trackerURL, "peers")
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, peersURL, nil)
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

	var peerResponses []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&peerResponses); err != nil {
		return nil, fmt.Errorf("failed to decode tracker response: %w", err)
	}

	var peers []peer.AddrInfo
	for _, p := range peerResponses {
		addrStr, ok := p["addr"].(string)
		if !ok {
			continue
		}

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

func (t *TrackerClient) RegisterPeer(ctx context.Context, multiaddrStr string, authData string) error {
	if t.trackerURL == "" {
		return nil
	}

	registerURL, err := joinTrackerPath(t.trackerURL, "register")
	if err != nil {
		return err
	}

	data := struct {
		Addr     string `json:"addr"`
		AuthData string `json:"auth_data,omitempty"`
	}{
		Addr:     multiaddrStr,
		AuthData: authData,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registerURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized: tracker requires authentication")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tracker registration failed: %d", resp.StatusCode)
	}
	return nil
}

func (t *TrackerClient) FetchTrackerAddr(ctx context.Context) (string, string, error) {
	if t.trackerURL == "" {
		return "", "", fmt.Errorf("no tracker URL configured")
	}

	addrURL, err := joinTrackerPath(t.trackerURL, "addr")
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addrURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("tracker request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("tracker returned status %d", resp.StatusCode)
	}

	var result struct {
		Libp2pAddr   string `json:"libp2p_addr"`
		RelayAddr    string `json:"relay_addr,omitempty"`
		RelayEnabled bool   `json:"relay_enabled"`
		DHTEnabled   bool   `json:"dht_enabled"`
		HostID       string `json:"host_id,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("failed to decode tracker response: %w", err)
	}
	return result.Libp2pAddr, result.RelayAddr, nil
}

func joinTrackerPath(baseURL, path string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid tracker URL: %w", err)
	}

	cleanPath := pathpkg.Clean(parsed.Path)
	if cleanPath == "/peers" || cleanPath == "/register" || cleanPath == "/addr" {
		parsed.Path = pathpkg.Dir(cleanPath)
	}

	joined, err := url.JoinPath(parsed.String(), path)
	if err != nil {
		return "", fmt.Errorf("invalid tracker path: %w", err)
	}
	return joined, nil
}
