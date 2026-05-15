package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func generateTestCert(notAfter time.Time) string {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     notAfter,
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
}

func TestParseCertExpiry_Invalid(t *testing.T) {
	if _, err := ParseCertExpiry("not a pem"); err == nil {
		t.Error("expected error for invalid PEM")
	}
	if _, err := ParseCertExpiry(""); err == nil {
		t.Error("expected error for empty PEM")
	}
}

func TestParseCertExpiry_Valid(t *testing.T) {
	notAfter := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	certPEM := generateTestCert(notAfter)

	expiry, err := ParseCertExpiry(certPEM)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !expiry.Equal(notAfter) {
		t.Errorf("expected %v, got %v", notAfter, expiry)
	}
}
