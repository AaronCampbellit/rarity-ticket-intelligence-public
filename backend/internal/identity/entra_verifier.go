package identity

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

type EntraVerifierConfig struct {
	TenantID string
	ClientID string
	Issuer   string
	JWKSURL  string
}

type EntraVerifier struct {
	config EntraVerifierConfig
	client *http.Client
	now    func() time.Time
	mu     sync.Mutex
	keys   map[string]*rsa.PublicKey
	until  time.Time
}

func NewEntraVerifier(
	config EntraVerifierConfig,
	client *http.Client,
	now func() time.Time,
) *EntraVerifier {
	if client == nil {
		client = &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	if now == nil {
		now = time.Now
	}
	return &EntraVerifier{config: config, client: client, now: now}
}

func (v *EntraVerifier) Verify(
	ctx context.Context,
	rawToken string,
	expectedNonce string,
) (Claims, error) {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 || expectedNonce == "" {
		return Claims{}, ErrUntrustedIdentity
	}
	headerData, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrUntrustedIdentity
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
		Type      string `json:"typ"`
	}
	if json.Unmarshal(headerData, &header) != nil ||
		header.Algorithm != "RS256" || header.KeyID == "" {
		return Claims{}, ErrUntrustedIdentity
	}
	key, err := v.key(ctx, header.KeyID, false)
	if err != nil {
		key, err = v.key(ctx, header.KeyID, true)
	}
	if err != nil {
		return Claims{}, ErrUntrustedIdentity
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrUntrustedIdentity
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) != nil {
		return Claims{}, ErrUntrustedIdentity
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrUntrustedIdentity
	}
	var tokenClaims entraTokenClaims
	if json.Unmarshal(payload, &tokenClaims) != nil {
		return Claims{}, ErrUntrustedIdentity
	}
	now := v.now().UTC()
	if tokenClaims.Issuer != v.config.Issuer ||
		tokenClaims.TenantID != v.config.TenantID ||
		!tokenClaims.Audience.Contains(v.config.ClientID) ||
		tokenClaims.Subject == "" || tokenClaims.Nonce != expectedNonce ||
		tokenClaims.ExpiresAt <= now.Add(-2*time.Minute).Unix() ||
		tokenClaims.NotBefore > now.Add(2*time.Minute).Unix() ||
		tokenClaims.IssuedAt > now.Add(2*time.Minute).Unix() {
		return Claims{}, ErrUntrustedIdentity
	}
	email := tokenClaims.Email
	if email == "" {
		email = tokenClaims.PreferredUsername
	}
	return Claims{
		Issuer: tokenClaims.Issuer, TenantID: tokenClaims.TenantID,
		Audience: v.config.ClientID, Subject: tokenClaims.Subject,
		Email: email, DisplayName: tokenClaims.Name,
	}, nil
}

type audienceClaim []string

func (a *audienceClaim) UnmarshalJSON(data []byte) error {
	var single string
	if json.Unmarshal(data, &single) == nil {
		*a = []string{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

func (a audienceClaim) Contains(expected string) bool {
	for _, value := range a {
		if value == expected {
			return true
		}
	}
	return false
}

type entraTokenClaims struct {
	Issuer            string        `json:"iss"`
	TenantID          string        `json:"tid"`
	Audience          audienceClaim `json:"aud"`
	Subject           string        `json:"sub"`
	Email             string        `json:"email"`
	PreferredUsername string        `json:"preferred_username"`
	Name              string        `json:"name"`
	Nonce             string        `json:"nonce"`
	ExpiresAt         int64         `json:"exp"`
	NotBefore         int64         `json:"nbf"`
	IssuedAt          int64         `json:"iat"`
}

func (v *EntraVerifier) key(
	ctx context.Context,
	keyID string,
	refresh bool,
) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !refresh && v.keys != nil && v.until.After(v.now()) {
		if key := v.keys[keyID]; key != nil {
			return key, nil
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.config.JWKSURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := v.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("JWKS request failed")
	}
	var document struct {
		Keys []struct {
			KeyID string   `json:"kid"`
			Type  string   `json:"kty"`
			Use   string   `json:"use"`
			N     string   `json:"n"`
			E     string   `json:"e"`
			X5C   []string `json:"x5c"`
		} `json:"keys"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, (1<<20)+1))
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, candidate := range document.Keys {
		if candidate.KeyID == "" || candidate.Type != "RSA" ||
			candidate.Use != "" && candidate.Use != "sig" {
			continue
		}
		key, err := parseRSAKey(candidate.N, candidate.E, candidate.X5C)
		if err == nil {
			keys[candidate.KeyID] = key
		}
	}
	v.keys, v.until = keys, v.now().Add(time.Hour)
	key := keys[keyID]
	if key == nil {
		return nil, errors.New("signing key not found")
	}
	return key, nil
}

func parseRSAKey(modulus, exponent string, certificates []string) (*rsa.PublicKey, error) {
	if len(certificates) > 0 {
		der, err := base64.StdEncoding.DecodeString(certificates[0])
		if err == nil {
			certificate, err := x509.ParseCertificate(der)
			if err == nil {
				if key, ok := certificate.PublicKey.(*rsa.PublicKey); ok {
					return key, nil
				}
			}
		}
	}
	n, err := base64.RawURLEncoding.DecodeString(modulus)
	if err != nil {
		return nil, err
	}
	e, err := base64.RawURLEncoding.DecodeString(exponent)
	if err != nil || len(e) == 0 || len(e) > 4 {
		return nil, errors.New("invalid RSA exponent")
	}
	exponentValue := 0
	for _, value := range e {
		exponentValue = exponentValue<<8 | int(value)
	}
	if exponentValue < 3 {
		return nil, errors.New("invalid RSA exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponentValue}, nil
}
