// Package secrets defines an encryption-provider boundary for protected
// configuration values. LocalProvider supports local/demo use; production key
// custody can implement the same Provider contract.
package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

var (
	ErrInvalidKey = errors.New("invalid encryption key")
	ErrOpen       = errors.New("unable to open protected value")
)

type SealedValue struct {
	Version    int
	Nonce      []byte
	Ciphertext []byte
}

type Provider interface {
	Seal(context.Context, string, []byte) (SealedValue, error)
	Open(context.Context, string, SealedValue) ([]byte, error)
}

type LocalProvider struct {
	aead        cipher.AEAD
	randomBytes func(int) ([]byte, error)
}

func NewLocalProvider(key []byte, randomBytes func(int) ([]byte, error)) (*LocalProvider, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrInvalidKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrInvalidKey
	}
	if randomBytes == nil {
		randomBytes = cryptoRandomBytes
	}
	return &LocalProvider{aead: aead, randomBytes: randomBytes}, nil
}

func (p *LocalProvider) Seal(_ context.Context, purpose string, plaintext []byte) (SealedValue, error) {
	nonce, err := p.randomBytes(p.aead.NonceSize())
	if err != nil {
		return SealedValue{}, err
	}
	ciphertext := p.aead.Seal(nil, nonce, plaintext, []byte(purpose))
	return SealedValue{Version: 1, Nonce: nonce, Ciphertext: ciphertext}, nil
}

func (p *LocalProvider) Open(_ context.Context, purpose string, value SealedValue) ([]byte, error) {
	if value.Version != 1 || len(value.Nonce) != p.aead.NonceSize() {
		return nil, ErrOpen
	}
	plaintext, err := p.aead.Open(nil, value.Nonce, value.Ciphertext, []byte(purpose))
	if err != nil {
		return nil, ErrOpen
	}
	return plaintext, nil
}

func cryptoRandomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return nil, err
	}
	return value, nil
}
