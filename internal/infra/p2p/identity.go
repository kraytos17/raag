package p2p

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

const identityKeyFile = "identity.key"

type IdentityManager struct {
	dataDir string
	keyFile string
}

func NewIdentityManager(dataDir string) *IdentityManager {
	return &IdentityManager{
		dataDir: dataDir,
		keyFile: filepath.Join(dataDir, identityKeyFile),
	}
}

func (m *IdentityManager) LoadOrCreate() (crypto.PrivKey, peer.ID, error) {
	keyData, err := os.ReadFile(m.keyFile)
	if err == nil {
		key, err := crypto.UnmarshalPrivateKey(keyData)
		if err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal identity key: %w", err)
		}

		id, err := peer.IDFromPrivateKey(key)
		if err != nil {
			return nil, "", fmt.Errorf("failed to generate peer ID: %w", err)
		}
		return key, id, nil
	}
	if !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("failed to read identity key: %w", err)
	}

	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate identity key: %w", err)
	}

	keyBytes, err := crypto.MarshalPrivateKey(key)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal identity key: %w", err)
	}
	if err := os.WriteFile(m.keyFile, keyBytes, 0600); err != nil {
		return nil, "", fmt.Errorf("failed to save identity key: %w", err)
	}

	id, err := peer.IDFromPrivateKey(key)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate peer ID: %w", err)
	}
	return key, id, nil
}
