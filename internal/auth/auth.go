package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/logger"
)

// AuthToken represents a strong authentication token with signature
type AuthToken struct {
	PeerID    string `json:"peer_id"`
	PublicKey string `json:"public_key"`
	Signature string `json:"signature"`
	Timestamp int64  `json:"timestamp"`
	ExpiresAt int64  `json:"expires_at"`
	Nonce     string `json:"nonce"`
}

// GenerateKeyPair generates a new ed25519 key pair for authentication
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// GenerateAuthKeyPair generates a new ed25519 private key for authentication
func GenerateAuthKeyPair() (ed25519.PrivateKey, error) {
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate auth key pair: %w", err)
	}
	return privKey, nil
}

// DeriveAuthKey derives an ed25519 auth key from identity key bytes
// This binds auth to identity
func DeriveAuthKey(identityKeyBytes []byte) (ed25519.PrivateKey, error) {
	salt := []byte("raag-auth-v1")
	h := sha256.New()
	h.Write(identityKeyBytes)
	h.Write(salt)
	seed := h.Sum(nil)

	privKey := ed25519.NewKeyFromSeed(seed)
	return privKey, nil
}

// GenerateToken creates a new authentication token for a peer
func GenerateToken(peerID peer.ID, pubKey ed25519.PrivateKey) (*AuthToken, error) {
	nonce, err := generateNonce()
	if err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	now := time.Now()
	token := &AuthToken{
		PeerID:    peerID.String(),
		PublicKey: hex.EncodeToString(pubKey.Public().(ed25519.PublicKey)),
		Timestamp: now.Unix(),
		ExpiresAt: now.Add(constants.TokenExpiration).Unix(),
		Nonce:     nonce,
	}

	signature, err := signToken(token, pubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign token: %w", err)
	}

	token.Signature = signature
	return token, nil
}

// SignToken signs an authentication token
func signToken(token *AuthToken, privKey ed25519.PrivateKey) (string, error) {
	payload := fmt.Sprintf("%s:%s:%d:%d:%s",
		token.PeerID,
		token.PublicKey,
		token.Timestamp,
		token.ExpiresAt,
		token.Nonce,
	)

	signature := ed25519.Sign(privKey, []byte(payload))
	return base64.StdEncoding.EncodeToString(signature), nil
}

// VerifyToken verifies an authentication token
func VerifyToken(token *AuthToken) (bool, error) {
	if token.PeerID == "" {
		return false, fmt.Errorf("empty peer ID")
	}
	if token.Signature == "" {
		return false, fmt.Errorf("missing signature")
	}
	if time.Now().Unix() > token.ExpiresAt {
		return false, fmt.Errorf("token expired")
	}

	pubKeyBytes, err := hex.DecodeString(token.PublicKey)
	if err != nil {
		return false, fmt.Errorf("invalid public key: %w", err)
	}

	payload := fmt.Sprintf("%s:%s:%d:%d:%s",
		token.PeerID,
		token.PublicKey,
		token.Timestamp,
		token.ExpiresAt,
		token.Nonce,
	)

	signature, err := base64.StdEncoding.DecodeString(token.Signature)
	if err != nil {
		return false, fmt.Errorf("invalid signature format: %w", err)
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return false, fmt.Errorf("invalid public key length: expected %d, got %d", ed25519.PublicKeySize, len(pubKeyBytes))
	}
	if !ed25519.Verify(ed25519.PublicKey(pubKeyBytes), []byte(payload), signature) {
		return false, fmt.Errorf("signature verification failed")
	}
	return true, nil
}

// GenerateNonce generates a random nonce
func generateNonce() (string, error) {
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// SerializeToken serializes a token to JSON
func SerializeToken(token *AuthToken) (string, error) {
	data, err := json.Marshal(token)
	if err != nil {
		return "", fmt.Errorf("failed to marshal token: %w", err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// DeserializeToken deserializes a token from JSON
func DeserializeToken(data string) (*AuthToken, error) {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode token: %w", err)
	}

	var token AuthToken
	if err := json.Unmarshal(decoded, &token); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token: %w", err)
	}
	return &token, nil
}

// TrustedKeyManager manages a list of trusted public keys
type TrustedKeyManager struct {
	trustedKeys map[string]struct{}
}

// NewTrustedKeyManager creates a new trusted key manager
func NewTrustedKeyManager() *TrustedKeyManager {
	return &TrustedKeyManager{
		trustedKeys: make(map[string]struct{}),
	}
}

// AddKey adds a public key to the trusted list
func (tk *TrustedKeyManager) AddKey(publicKeyHex string) {
	if _, exists := tk.trustedKeys[publicKeyHex]; !exists {
		tk.trustedKeys[publicKeyHex] = struct{}{}
		logger.Infof("Added trusted auth key: %s...", publicKeyHex[:16])
	}
}

// IsTrusted checks if a public key is trusted
func (tk *TrustedKeyManager) IsTrusted(publicKeyHex string) bool {
	_, trusted := tk.trustedKeys[publicKeyHex]
	return trusted
}

// VerifyAndCheckTrust verifies a token and checks if its key is trusted
func (tk *TrustedKeyManager) VerifyAndCheckTrust(token *AuthToken) (bool, error) {
	if !tk.IsTrusted(token.PublicKey) {
		return false, fmt.Errorf("public key not in trusted list")
	}
	return VerifyToken(token)
}
