package password

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestNewHashesRemainArgon2id(t *testing.T) {
	const plain = " fixture password with spaces "
	hash, err := Hash(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatal("new passwords must use Argon2id")
	}
	if err := Verify(hash, plain); err != nil {
		t.Fatal(err)
	}
	if err := Verify(hash, strings.TrimSpace(plain)); !errors.Is(err, ErrMismatched) {
		t.Fatalf("password was normalized or mismatch misclassified: %v", err)
	}
}

func TestLegacyBcryptCredentials(t *testing.T) {
	const plain = "legacy fixture password"
	encoded, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"$2a$", "$2b$", "$2y$"} {
		t.Run(variant, func(t *testing.T) {
			hash := variant + string(encoded[4:])
			if err := Verify(hash, plain); err != nil {
				t.Fatalf("existing bcrypt password rejected: %v", err)
			}
			if err := Verify(hash, "incorrect fixture password"); !errors.Is(err, ErrMismatched) {
				t.Fatalf("wrong password must be a normal mismatch: %v", err)
			}
		})
	}
}

func TestCorruptStoredCredentialsAreRejected(t *testing.T) {
	for _, hash := range []string{"", "$2b$truncated", "$argon2id$truncated", "unsupported-format"} {
		if err := Verify(hash, "fixture"); err == nil || errors.Is(err, ErrMismatched) {
			t.Fatalf("corrupt stored credential must fail parsing: %v", err)
		}
	}
}
