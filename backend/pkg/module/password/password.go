// Package password writes Argon2id hashes and verifies existing Argon2id or
// bcrypt hashes without changing the stored credential.
package password

import (
	"errors"
	"strings"

	"github.com/alexedwards/argon2id"
	"golang.org/x/crypto/bcrypt"
)

// ErrMismatched 密码与哈希不匹配
var ErrMismatched = errors.New("password mismatched")

// Hash 用 argon2id 生成 PHC 格式哈希字符串
func Hash(plain string) (string, error) {
	return argon2id.CreateHash(plain, argon2id.DefaultParams)
}

// Verify 校验密码，不匹配返回 ErrMismatched
func Verify(hash, plain string) error {
	if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$") {
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)); err != nil {
			if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
				return ErrMismatched
			}
			return err
		}
		return nil
	}
	ok, err := argon2id.ComparePasswordAndHash(plain, hash)
	if err != nil {
		return err
	}
	if !ok {
		return ErrMismatched
	}
	return nil
}
