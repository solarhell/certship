package model

import "encoding/base64"

const (
	DefaultLimit uint64 = 20
	MaxLimit     uint64 = 100
)

func EncodeCursor(id string) string {
	return base64.StdEncoding.EncodeToString([]byte(id))
}

func DecodeCursor(cursor string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func NormalizeLimit(limit uint64) uint64 {
	if limit == 0 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}
