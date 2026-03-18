package storage

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
)

type IdentityStore struct {
	store   *Store
	keyPath string
}

func NewIdentityStore(store *Store, keyPath string) *IdentityStore {
	return &IdentityStore{store: store, keyPath: keyPath}
}

func (i *IdentityStore) LoadOrGenerate(ctx context.Context) (crypto.PrivKey, error) {
	data, err := i.store.Get(ctx, "/identity/privatekey")
	if err == nil && len(data) > 0 {
		var sk serializedKey
		if err := json.Unmarshal(data, &sk); err == nil {
			key, err := crypto.UnmarshalPrivateKey(sk.Data)
			if err == nil {
				return key, nil
			}
		}
	}

	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	keyData, err := crypto.MarshalPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal key: %w", err)
	}

	sk := serializedKey{
		Type: "Ed25519",
		Data: keyData,
	}

	jsonData, err := json.Marshal(sk)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal key data: %w", err)
	}

	if err := i.store.Put(ctx, "/identity/privatekey", jsonData); err != nil {
		fmt.Printf("Warning: failed to save identity key to datastore: %v\n", err)
	}

	dir := filepath.Dir(i.keyPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Printf("Warning: failed to create key directory: %v\n", err)
	} else if err := os.WriteFile(i.keyPath, jsonData, 0o600); err != nil {
		fmt.Printf("Warning: failed to save identity key: %v\n", err)
	}

	return key, nil
}

func (i *IdentityStore) HasKey(ctx context.Context) (bool, error) {
	return i.store.Has(ctx, "/identity/privatekey")
}

func (i *IdentityStore) SaveLibP2PKey(ctx context.Context, key crypto.PrivKey) error {
	keyData, err := crypto.MarshalPrivateKey(key)
	if err != nil {
		return fmt.Errorf("failed to marshal key: %w", err)
	}

	sk := serializedKey{
		Type: "Ed25519",
		Data: keyData,
	}

	jsonData, err := json.Marshal(sk)
	if err != nil {
		return fmt.Errorf("failed to marshal key data: %w", err)
	}

	return i.store.Put(ctx, "/identity/privatekey", jsonData)
}

type serializedKey struct {
	Type string `json:"type"`
	Data []byte `json:"data"`
}
