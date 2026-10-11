package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Host passwords must be usable (Hostwarden logs in to ESXi with them), so
// they cannot be hashed; they are encrypted with AES-256-GCM instead.
//
// The key comes from HOSTWARDEN_SECRET_KEY when set, otherwise from the file
// secret.key next to the store (created on first use, mode 0600). This
// protects a store.json that leaks on its own - a backup, a copy, a support
// bundle. It does not protect against someone who can read the whole data
// directory: keep the key elsewhere (the environment variable) for that.

const (
	secretEnv    = "HOSTWARDEN_SECRET_KEY"
	secretFile   = "secret.key"
	sealedPrefix = "enc:v1:"
)

type secretBox struct {
	aead cipher.AEAD
}

func loadSecretBox(dataDir string) (*secretBox, error) {
	var key [32]byte
	env := os.Getenv(secretEnv)
	if env == "" {
		env = os.Getenv("FREECENTER_SECRET_KEY") // name used before the rename
	}
	if env != "" {
		key = sha256.Sum256([]byte(env))
	} else {
		path := filepath.Join(dataDir, secretFile)
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			fresh := make([]byte, 32)
			if _, err := rand.Read(fresh); err != nil {
				return nil, err
			}
			raw = []byte(hex.EncodeToString(fresh))
			if err := os.WriteFile(path, raw, 0600); err != nil {
				return nil, fmt.Errorf("create %s: %w", path, err)
			}
		} else if err != nil {
			return nil, err
		}
		key = sha256.Sum256([]byte(strings.TrimSpace(string(raw))))
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &secretBox{aead: aead}, nil
}

// seal encrypts a secret for storage; the empty string stays empty.
func (b *secretBox) seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return sealedPrefix + base64.StdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// open decrypts a stored secret. A value without the prefix is a plain-text
// secret written by an older version: it is returned as is, with
// wasEncrypted=false, so the caller can rewrite it.
func (b *secretBox) open(stored string) (plain string, wasEncrypted bool, err error) {
	rest, ok := strings.CutPrefix(stored, sealedPrefix)
	if !ok {
		return stored, false, nil
	}
	raw, err := base64.StdEncoding.DecodeString(rest)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", true, errors.New("malformed encrypted value")
	}
	nonce, ciphertext := raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():]
	out, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", true, errors.New("wrong key or corrupted value")
	}
	return string(out), true, nil
}
