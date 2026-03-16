package tracker

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func GenerateSelfSignedCert() (certFile, keyFile string, err error) {
	certDir := "/app/certs"
	if err := os.MkdirAll(certDir, 0o750); err != nil {
		return "", "", fmt.Errorf("failed to create certs directory: %w", err)
	}

	root, err := os.OpenRoot(certDir)
	if err != nil {
		return "", "", fmt.Errorf("error opening cert root: %w", err)
	}

	certTmpFile, err := root.Open("raag-tracker.crt")
	if err != nil {
		return "", "", fmt.Errorf("failed to create cert file: %w", err)
	}
	defer certTmpFile.Close()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate private key: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Raag P2P Music"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return "", "", fmt.Errorf("failed to create certificate: %w", err)
	}

	if err := pem.Encode(certTmpFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return "", "", fmt.Errorf("failed to encode cert: %w", err)
	}

	keyTmpFile, err := root.Open("raag-key.pem")
	if err != nil {
		return "", "", fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyTmpFile.Close()

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal key: %w", err)
	}

	if err := pem.Encode(keyTmpFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		return "", "", fmt.Errorf("failed to encode key: %w", err)
	}

	certFile = filepath.Join(certDir, "raag-tracker.crt")
	keyFile = filepath.Join(certDir, "raag-tracker.key")
	return certFile, keyFile, nil
}

func LoadOrGenerateCerts(certFile, keyFile string) (string, string, error) {
	if certFile != "" && keyFile != "" {
		certFile = filepath.Clean(certFile)
		keyFile = filepath.Clean(keyFile)
		certAbs, err := filepath.Abs(certFile)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve cert path: %w", err)
		}

		keyAbs, err := filepath.Abs(keyFile)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve key path: %w", err)
		}

		certFile = certAbs
		keyFile = keyAbs
		if _, err := os.Stat(certFile); err == nil {
			if _, err := os.Stat(keyFile); err == nil {
				return certFile, keyFile, nil
			}
		}
		return "", "", fmt.Errorf("TLS cert/key files not found")
	}

	certFile, keyFile, err := GenerateSelfSignedCert()
	if err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}

func GetTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		MaxVersion:   tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{
			tls.CurveP256,
			tls.X25519,
		},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
	}, nil
}
