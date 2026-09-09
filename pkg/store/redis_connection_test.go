package store

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rgehrsitz/rex/pkg/logging"
)

func TestNewRedisStoreReturnsConnectionFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	factStore, err := NewRedisStore(ctx, RedisOptions{Addr: address, DialTimeout: 50 * time.Millisecond})
	assert.Nil(t, factStore)
	assert.ErrorContains(t, err, "connect to Redis")
}

func TestNewRedisStoreHonorsCanceledStartup(t *testing.T) {
	server, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	factStore, err := NewRedisStore(ctx, RedisOptions{Addr: server.Addr()})
	assert.Nil(t, factStore)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestNewRedisStoreUsesVerifiedTLS(t *testing.T) {
	serverTLS, roots := testTLSConfig(t)
	server, err := miniredis.RunTLS(serverTLS)
	require.NoError(t, err)
	t.Cleanup(server.Close)

	factStore, err := NewRedisStore(context.Background(), RedisOptions{
		Addr: server.Addr(), TLSConfig: &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12},
	})
	require.NoError(t, err)
	require.NoError(t, factStore.Close())

	factStore, err = NewRedisStore(context.Background(), RedisOptions{
		Addr: server.Addr(), TLSConfig: &tls.Config{RootCAs: roots, ServerName: "wrong.example", MinVersion: tls.VersionTLS12},
	})
	assert.Nil(t, factStore)
	assert.Error(t, err)
}

func TestNewRedisStoreDoesNotLogCredentials(t *testing.T) {
	server, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(server.Close)
	// These values are generated from the test name and exist only in miniredis.
	username := t.Name() + "-user"
	password := t.Name() + "-password"
	server.RequireUserAuth(username, password)
	var output bytes.Buffer
	previous := logging.Logger
	logging.Logger = zerolog.New(&output)
	t.Cleanup(func() { logging.Logger = previous })

	factStore, err := NewRedisStore(context.Background(), RedisOptions{Addr: server.Addr(), Username: username, Password: password})
	require.NoError(t, err)
	require.NoError(t, factStore.Close())
	assert.NotContains(t, output.String(), password)
	assert.NotContains(t, output.String(), username)
}

func TestRedisLogAddressRemovesUnexpectedUserInfo(t *testing.T) {
	assert.Equal(t, "redis.example:6380", redisLogAddress("redis://user:secret@redis.example:6380"))
}

func testTLSConfig(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	certificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	certificateValue, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	roots.AddCert(certificateValue)
	return &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}, roots
}
