package acme

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/go-acme/lego/v5/certcrypto"
	"go.uber.org/zap"
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

func TestEnsureAccountReusesStoredRegistration(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := string(pem.EncodeToMemory(certcrypto.PEMBlock(key)))
	const accountURL = "https://acme.invalid/acct/123"
	for _, tc := range []struct {
		name string
		json string
	}{
		{"lego v5", `{"accountURL":"https://acme.invalid/acct/123","status":"valid","contact":["mailto:acme@certship.dev"],"orders":"https://acme.invalid/orders/123"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Existing registrations must return without contacting a CA or generating a new key.
			account, err := NewManager(zap.NewNop()).EnsureAccount(t.Context(), keyPEM, tc.json)
			if err != nil {
				t.Fatal(err)
			}
			if account.Registered || account.RegistrationJSON != tc.json {
				t.Fatal("existing account was replaced or marked for re-registration")
			}
			reg := account.user.GetRegistration()
			if reg.Location != accountURL || reg.Status != "valid" || reg.Orders != "https://acme.invalid/orders/123" || len(reg.Contact) != 1 || reg.Contact[0] != "mailto:acme@certship.dev" {
				t.Fatalf("registration metadata was not preserved: %+v", reg)
			}
			restored, err := certcrypto.ParsePEMPrivateKey([]byte(account.KeyPEM))
			if err != nil {
				t.Fatal(err)
			}
			want, err := x509.MarshalPKIXPublicKey(key.Public())
			if err != nil {
				t.Fatal(err)
			}
			got, err := x509.MarshalPKIXPublicKey(restored.Public())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatal("stored account key changed")
			}
		})
	}
}

func TestBuildUserRejectsInvalidStoredAccount(t *testing.T) {
	mgr := NewManager(zap.NewNop())
	u, err := mgr.buildUser("", "")
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := string(pem.EncodeToMemory(certcrypto.PEMBlock(u.key)))
	for _, raw := range []string{`{`, `null`, `{}`, `{"status":"valid"}`, `{"accountURL":123}`, `{"uri":"https://acme.invalid/acct/1","body":{"status":"valid"}}`} {
		t.Run(raw, func(t *testing.T) {
			if _, err := mgr.buildUser(keyPEM, raw); err == nil {
				t.Fatal("invalid registration must not silently trigger a new registration")
			}
		})
	}
	if _, err := mgr.buildUser("", `{"accountURL":"https://acme.invalid/acct/1","status":"valid"}`); err == nil {
		t.Fatal("stored registration without its original key must be rejected")
	}
	if _, err := mgr.buildUser("not a key", ""); err == nil {
		t.Fatal("invalid stored key must be rejected")
	}
}

func TestObtainCertCanceledBeforeStarting(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewManager(zap.NewNop()).ObtainCert(ctx, nil, ObtainOptions{}); err != context.Canceled {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestEnsureAccountCanceledBeforeStarting(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewManager(zap.NewNop()).EnsureAccount(ctx, "invalid key", ""); err != context.Canceled {
		t.Fatalf("got %v, want context.Canceled before any account setup", err)
	}
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
