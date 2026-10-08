package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEntraVerifierValidatesSignatureTenantAudienceTimeAndNonce(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	exponent := big.NewInt(int64(key.PublicKey.E)).Bytes()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{"keys": []map[string]any{{
			"kid": "key-id", "kty": "RSA", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(exponent),
		}}})
	}))
	defer server.Close()
	at := time.Date(2026, time.July, 30, 22, 0, 0, 0, time.UTC)
	verifier := NewEntraVerifier(EntraVerifierConfig{
		TenantID: "tenant-id", ClientID: "client-id",
		Issuer:  "https://login.microsoftonline.com/tenant-id/v2.0",
		JWKSURL: server.URL,
	}, server.Client(), func() time.Time { return at })
	token := signTestJWT(t, key, map[string]any{
		"iss": "https://login.microsoftonline.com/tenant-id/v2.0",
		"tid": "tenant-id", "aud": "client-id", "sub": "subject-id",
		"preferred_username": "tech@example.com", "name": "Taylor Tech",
		"nonce": "expected-nonce", "iat": at.Unix(), "nbf": at.Unix(),
		"exp": at.Add(time.Hour).Unix(),
	})
	claims, err := verifier.Verify(context.Background(), token, "expected-nonce")
	if err != nil || claims.Subject != "subject-id" || claims.Email != "tech@example.com" {
		t.Fatalf("Verify() claims=%+v error=%v", claims, err)
	}
	if _, err := verifier.Verify(context.Background(), token, "wrong-nonce"); err == nil {
		t.Fatal("wrong nonce was accepted")
	}
}

func signTestJWT(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{
		"alg": "RS256", "kid": "key-id", "typ": "JWT",
	})
	payload, _ := json.Marshal(claims)
	unsigned := fmt.Sprintf("%s.%s",
		base64.RawURLEncoding.EncodeToString(header),
		base64.RawURLEncoding.EncodeToString(payload))
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}
