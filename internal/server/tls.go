package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

// certPaths returns the paths to the generated cert and key files.
func certPaths() (certFile, keyFile string) {
	dir := store.SubDir(store.DirCerts)
	return filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
}

// TLSFiles returns the cert and key files the server serves with: the
// configured ones when set, otherwise the self-signed pair, which is
// generated or regenerated first when it does not cover the host's names.
func TLSFiles(t config.TLSConfig, regenerate bool) (certFile, keyFile string, err error) {
	if t.Cert != "" && t.Key != "" {
		if _, err := tls.LoadX509KeyPair(t.Cert, t.Key); err != nil {
			return "", "", err
		}
		return t.Cert, t.Key, nil
	}
	certFile, keyFile = certPaths()
	_, err = loadOrGenerate(t, regenerate)
	return certFile, keyFile, err
}

// LoadOrGenerateTLS loads the configured TLS cert+key, or the self-signed
// one, generating a new self-signed cert when none exists or the existing
// one does not cover every name in SelfSignedSANs.
func LoadOrGenerateTLS(t config.TLSConfig) (tls.Certificate, error) {
	if t.Cert != "" && t.Key != "" {
		return tls.LoadX509KeyPair(t.Cert, t.Key)
	}
	return loadOrGenerate(t, false)
}

func loadOrGenerate(t config.TLSConfig, regenerate bool) (tls.Certificate, error) {
	certFile, keyFile := certPaths()
	unlock, err := lockCerts(filepath.Dir(certFile))
	if err != nil {
		return tls.Certificate{}, err
	}
	defer unlock()

	if regenerate {
		return generateSelfSigned(certFile, keyFile, t.SANs)
	}
	if cert, err := tls.LoadX509KeyPair(certFile, keyFile); err == nil && certCovers(cert, SelfSignedSANs(t.SANs)) {
		return cert, nil
	}
	return generateSelfSigned(certFile, keyFile, t.SANs)
}

// lockCerts serializes self-signed cert generation across processes, such as
// the dev server and the Vite dev cert helper starting together, so they
// never write a mismatched cert and key.
func lockCerts(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating cert dir: %w", err)
	}
	lock := filepath.Join(dir, ".lock")
	deadline := time.Now().Add(15 * time.Second)
	for {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(lock) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("locking cert dir: %w", err)
		}
		// A lock left behind by a crashed process is stale.
		if info, statErr := os.Stat(lock); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for cert lock %s", lock)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// SelfSignedSANs returns the names a generated cert must cover: localhost,
// the loopback addresses, the hostname, the host's current non-loopback IPv4
// addresses, and any extra configured names.
func SelfSignedSANs(extra []string) []string {
	sans := []string{"localhost", "127.0.0.1", "::1"}
	if host, err := os.Hostname(); err == nil && host != "" {
		sans = append(sans, strings.ToLower(host))
	}
	sans = append(sans, lanIPv4s()...)
	for _, name := range extra {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			sans = append(sans, name)
		}
	}
	var out []string
	for _, name := range sans {
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// lanIPv4s returns the IPv4 addresses of the host's up, non-loopback interfaces.
func lanIPv4s() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var ips []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipnet.IP.To4(); ip4 != nil && !ip4.IsLoopback() && !ip4.IsLinkLocalUnicast() {
				ips = append(ips, ip4.String())
			}
		}
	}
	return ips
}

// certCovers reports whether cert is unexpired and lists every name in sans.
func certCovers(cert tls.Certificate, sans []string) bool {
	if len(cert.Certificate) == 0 {
		return false
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil || time.Now().After(leaf.NotAfter) {
		return false
	}
	for _, name := range sans {
		if ip := net.ParseIP(name); ip != nil {
			if !slices.ContainsFunc(leaf.IPAddresses, ip.Equal) {
				return false
			}
		} else if !slices.Contains(leaf.DNSNames, name) {
			return false
		}
	}
	return true
}

func generateSelfSigned(certFile, keyFile string, extraSANs []string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generating key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generating serial: %w", err)
	}

	var dnsNames []string
	var ips []net.IP
	for _, name := range SelfSignedSANs(extraSANs) {
		if ip := net.ParseIP(name); ip != nil {
			ips = append(ips, ip)
		} else {
			dnsNames = append(dnsNames, name)
		}
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Aviary"},
			CommonName:   "localhost",
		},
		DNSNames:    dnsNames,
		IPAddresses: ips,
		NotBefore:   time.Now().Add(-time.Minute),
		// Browsers reject leaf certs valid for more than 825 days.
		NotAfter:              time.Now().Add(825 * 24 * time.Hour),
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
