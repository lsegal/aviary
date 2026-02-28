package server

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
	"os"
	"path/filepath"
	"time"

	"github.com/lsegal/aviary/internal/store"
)

// certPaths returns the paths to the cert and key files.
func certPaths() (certFile, keyFile string) {
	dir := store.SubDir(store.DirCerts)
	return filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
}

// LoadOrGenerateTLS loads the existing TLS cert+key or generates a new self-signed one.
func LoadOrGenerateTLS(customCert, customKey string) (tls.Certificate, error) {
	if customCert != "" && customKey != "" {
		return tls.LoadX509KeyPair(customCert, customKey)
	}

	certFile, keyFile := certPaths()

	// Try to load existing cert.
	if cert, err := tls.LoadX509KeyPair(certFile, keyFile); err == nil {
		return cert, nil
	}

	// Generate a new self-signed cert.
	return generateSelfSigned(certFile, keyFile)
}

func generateSelfSigned(certFile, keyFile string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generating key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generating serial: %w", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Aviary"},
			CommonName:   "localhost",
		},
		DNSNames:              []string{"localhost"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("creating certificate: %w", err)
	}

	// Persist cert.
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return tls.Certificate{}, fmt.Errorf("creating cert dir: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("writing cert: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshaling key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("writing key: %w", err)
	}

	return tls.X509KeyPair(certPEM, keyPEM)
}
