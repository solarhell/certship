// Package password 封装密码哈希与校验，使用 argon2id
package password

import (
	"errors"

	"github.com/alexedwards/argon2id"
)

// ErrMismatched 密码与哈希不匹配
var ErrMismatched = errors.New("password mismatched")

// Hash 用 argon2id 生成 PHC 格式哈希字符串
func Hash(plain string) (string, error) {
	return argon2id.CreateHash(plain, argon2id.DefaultParams)
}

// Verify 校验密码，不匹配返回 ErrMismatched
func Verify(hash, plain string) error {
	ok, err := argon2id.ComparePasswordAndHash(plain, hash)
	if err != nil {
		return err
	}
	if !ok {
		return ErrMismatched
	}
	return nil
}
