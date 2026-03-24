package p2p

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/zalando/go-keyring"
)

const (
	identityKeyFile = "identity.key"
	serviceName     = "raag.libp2p"
	identityKey     = "identity"
)

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
	if err := m.ensureDirPerms(); err != nil {
		return nil, "", err
	}
	if key, id, err := m.tryKeychain(); err == nil {
		return key, id, nil
	}
	if key, id, err := m.tryFile(); err == nil {
		return key, id, nil
	}

	slog.Debug("no existing key found, generating new identity")
	return m.createNew()
}

func (m *IdentityManager) tryKeychain() (crypto.PrivKey, peer.ID, error) {
	data, err := keyring.Get(serviceName, identityKey)
	if err != nil {
		return nil, "", err
	}

	keyBytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode identity key: %w", err)
	}

	key, err := crypto.UnmarshalPrivateKey(keyBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to unmarshal identity key: %w", err)
	}

	id, err := peer.IDFromPrivateKey(key)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate peer ID: %w", err)
	}
	return key, id, nil
}

func (m *IdentityManager) tryFile() (crypto.PrivKey, peer.ID, error) {
	data, err := os.ReadFile(m.keyFile)
	if err != nil {
		return nil, "", err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, "", fmt.Errorf("failed to decode PEM block")
	}

	key, err := crypto.UnmarshalPrivateKey(block.Bytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to unmarshal identity key: %w", err)
	}

	id, err := peer.IDFromPrivateKey(key)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate peer ID: %w", err)
	}
	return key, id, nil
}

func (m *IdentityManager) createNew() (crypto.PrivKey, peer.ID, error) {
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate identity key: %w", err)
	}

	keyBytes, err := crypto.MarshalPrivateKey(key)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal identity key: %w", err)
	}
	defer wipeBytes(keyBytes)

	if err := m.storeKey(keyBytes); err != nil {
		return nil, "", err
	}

	id, err := peer.IDFromPrivateKey(key)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate peer ID: %w", err)
	}
	return key, id, nil
}

func (m *IdentityManager) storeKey(keyBytes []byte) error {
	err := m.storeKeychain(keyBytes)
	if err == nil {
		return nil
	}

	slog.Warn("OS keychain unavailable, using local file storage", "error", err)
	if writeErr := m.writeFile(keyBytes); writeErr != nil {
		return writeErr
	}
	return nil
}

func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func (m *IdentityManager) storeKeychain(keyBytes []byte) error {
	encoded := base64.StdEncoding.EncodeToString(keyBytes)
	return keyring.Set(serviceName, identityKey, encoded)
}

func (m *IdentityManager) writeFile(data []byte) error {
	if err := m.ensureDirPerms(); err != nil {
		return err
	}

	block := &pem.Block{
		Type:  "LIBP2P PRIVATE KEY",
		Bytes: data,
	}

	file, err := os.OpenFile(m.keyFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create identity file: %w", err)
	}
	defer file.Close()

	if err := pem.Encode(file, block); err != nil {
		return fmt.Errorf("failed to write identity file: %w", err)
	}
	return nil
}

func (m *IdentityManager) ensureDirPerms() error {
	info, err := os.Stat(m.dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(m.dataDir, 0o700)
		}
		return err
	}
	if info.IsDir() {
		if info.Mode().Perm() != 0o700 {
			if err := os.Chmod(m.dataDir, 0o700); err != nil {
				return fmt.Errorf("failed to set directory permissions: %w", err)
			}
		}
	}
	return nil
}
