package cdn

import (
	"testing"

	"go.uber.org/zap"
)

func TestNewManager(t *testing.T) {
	logger := zap.NewNop()
	m := NewManager(logger)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestIsCDNDomain_InvalidCredentials(t *testing.T) {
	logger := zap.NewNop()
	m := NewManager(logger)
	// Invalid credentials should return false, not panic
	result := m.IsCDNDomain("invalid", "invalid", "example.com")
	if result {
		t.Error("expected false for invalid credentials")
	}
}
