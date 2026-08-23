package keystore

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/secret"
)

const valuePrefix = "kolk-b64:"

func encodeValue(value secret.Secret) (string, error) {
	raw := value.Reveal()
	if raw == "" {
		return "", ErrEmpty
	}
	if len(raw) > MaxValueBytes {
		return "", ErrTooLarge
	}
	return valuePrefix + base64.StdEncoding.EncodeToString([]byte(raw)), nil
}

func decodeValue(encoded string) (secret.Secret, error) {
	if !strings.HasPrefix(encoded, valuePrefix) {
		return secret.Secret{}, fmt.Errorf("credential value has no encoding tag: %w", ErrCorrupt)
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, valuePrefix))
	if err != nil || len(b) == 0 {
		return secret.Secret{}, fmt.Errorf("credential value is not valid base64: %w", ErrCorrupt)
	}
	if len(b) > MaxValueBytes {
		return secret.Secret{}, ErrTooLarge
	}
	return secret.New(string(b)), nil
}

func hashValue(value secret.Secret) string {
	sum := sha256.Sum256([]byte(value.Reveal()))
	return hex.EncodeToString(sum[:])
}
