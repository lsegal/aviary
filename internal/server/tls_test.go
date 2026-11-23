package server

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
)

func leafOf(t *testing.T, cert tls.Certificate) *x509.Certificate {
	t.Helper()
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	return leaf
}

func TestLoadOrGenerateTLS_GeneratesWithHostSANs(t *testing.T) {
	setupServerDataDir(t)

	cert, err := LoadOrGenerateTLS(config.TLSConfig{SANs: []string{"Aviary.example", "203.0.113.7"}})
	require.NoError(t, err)
	leaf := leafOf(t, cert)

	assert.Contains(t, leaf.DNSNames, "localhost")
	assert.Contains(t, leaf.DNSNames, "aviary.example")
	host, err := os.Hostname()
	require.NoError(t, err)
	assert.Contains(t, leaf.DNSNames, strings.ToLower(host))

	ips := map[string]bool{}
	for _, ip := range leaf.IPAddresses {
		ips[ip.String()] = true
	}
	for _, want := range append([]string{"127.0.0.1", "::1", "203.0.113.7"}, lanIPv4s()...) {
		assert.True(t, ips[want], "cert should cover %s, has %v", want, leaf.IPAddresses)
	}
}

func TestLoadOrGenerateTLS_ReusesCoveringCert(t *testing.T) {
	setupServerDataDir(t)

	cert1, err := LoadOrGenerateTLS(config.TLSConfig{})
	require.NoError(t, err)
	cert2, err := LoadOrGenerateTLS(config.TLSConfig{})
	require.NoError(t, err)
	assert.Equal(t, cert1.Certificate[0], cert2.Certificate[0])
}

func TestLoadOrGenerateTLS_RegeneratesWhenSANsNotCovered(t *testing.T) {
	setupServerDataDir(t)

	cert1, err := LoadOrGenerateTLS(config.TLSConfig{})
	require.NoError(t, err)
	// A name the existing cert lacks, such as a new LAN address, forces a new cert.
	cert2, err := LoadOrGenerateTLS(config.TLSConfig{SANs: []string{"192.0.2.44"}})
	require.NoError(t, err)
	assert.NotEqual(t, cert1.Certificate[0], cert2.Certificate[0])
	assert.True(t, slicesContainsIP(leafOf(t, cert2).IPAddresses, "192.0.2.44"))

	// A cert that covers more than needed is still reused.
	cert3, err := LoadOrGenerateTLS(config.TLSConfig{})
	require.NoError(t, err)
	assert.Equal(t, cert2.Certificate[0], cert3.Certificate[0])
}

func TestLoadOrGenerateTLS_CustomCert(t *testing.T) {
	setupServerDataDir(t)

	_, err := LoadOrGenerateTLS(config.TLSConfig{Cert: "/nonexistent/cert.pem", Key: "/nonexistent/key.pem"})
	assert.Error(t, err)

	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	want, err := generateSelfSigned(certFile, keyFile, nil)
	require.NoError(t, err)
	got, err := LoadOrGenerateTLS(config.TLSConfig{Cert: certFile, Key: keyFile})
	require.NoError(t, err)
	assert.Equal(t, want.Certificate[0], got.Certificate[0])

	gotCert, gotKey, err := TLSFiles(config.TLSConfig{Cert: certFile, Key: keyFile}, true)
	require.NoError(t, err)
	assert.Equal(t, certFile, gotCert)
	assert.Equal(t, keyFile, gotKey)
}

func TestTLSFiles_Regenerate(t *testing.T) {
	setupServerDataDir(t)

	certFile, keyFile, err := TLSFiles(config.TLSConfig{}, false)
	require.NoError(t, err)
	first, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)

	_, _, err = TLSFiles(config.TLSConfig{}, false)
	require.NoError(t, err)
	same, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)
	assert.Equal(t, first.Certificate[0], same.Certificate[0])

	_, _, err = TLSFiles(config.TLSConfig{}, true)
	require.NoError(t, err)
	fresh, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)
	assert.NotEqual(t, first.Certificate[0], fresh.Certificate[0])
}

func TestLockCerts_RemovesStaleLock(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, ".lock")
	require.NoError(t, os.WriteFile(lock, nil, 0o600))
	old := time.Now().Add(-time.Minute)
	require.NoError(t, os.Chtimes(lock, old, old))

	unlock, err := lockCerts(dir)
	require.NoError(t, err)
	unlock()
	_, err = os.Stat(lock)
	assert.True(t, os.IsNotExist(err))
}

func TestTLSConfigChanged(t *testing.T) {
	assert.False(t, tlsConfigChanged(config.TLSConfig{}, config.TLSConfig{}))
	assert.False(t, tlsConfigChanged(config.TLSConfig{Cert: "c", Key: "k", SANs: []string{"a"}}, config.TLSConfig{Cert: "c", Key: "k", SANs: []string{"a"}}))
	assert.True(t, tlsConfigChanged(config.TLSConfig{Cert: "a"}, config.TLSConfig{Cert: "b"}))
	assert.True(t, tlsConfigChanged(config.TLSConfig{}, config.TLSConfig{SANs: []string{"nas.local"}}))
}

func slicesContainsIP(ips []net.IP, want string) bool {
	for _, ip := range ips {
		if ip.String() == want {
			return true
		}
	}
	return false
}
