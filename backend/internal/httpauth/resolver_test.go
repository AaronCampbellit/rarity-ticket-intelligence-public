package httpauth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

func TestMultiplexBearerRoutesOnlyServiceKeyPrefixToKeyResolver(t *testing.T) {
	sessionCalls, keyCalls := 0, 0
	resolve := MultiplexBearer(
		func(*http.Request) (authorization.Principal, error) {
			sessionCalls++
			return authorization.Principal{ID: "session"}, nil
		},
		func(*http.Request) (authorization.Principal, error) {
			keyCalls++
			return authorization.Principal{ID: "key"}, nil
		},
	)
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("Authorization", "Bearer rsk_prefix_secret")
	principal, err := resolve(request)
	if err != nil || principal.ID != "key" || keyCalls != 1 || sessionCalls != 0 {
		t.Fatalf("service key dispatch failed: principal=%+v err=%v calls=%d/%d", principal, err, keyCalls, sessionCalls)
	}

	request.Header.Set("Authorization", "Bearer opaque-session")
	principal, err = resolve(request)
	if err != nil || principal.ID != "session" || sessionCalls != 1 {
		t.Fatalf("session dispatch failed: principal=%+v err=%v", principal, err)
	}
}

func TestMultiplexBearerDoesNotFallbackAfterInvalidServiceKey(t *testing.T) {
	sessionCalls := 0
	resolve := MultiplexBearer(
		func(*http.Request) (authorization.Principal, error) {
			sessionCalls++
			return authorization.Principal{}, nil
		},
		func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{}, errors.New("invalid key")
		},
	)
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("Authorization", "Bearer rsk_invalid_secret")
	if _, err := resolve(request); err == nil || sessionCalls != 0 {
		t.Fatalf("invalid service key fell back to session: err=%v calls=%d", err, sessionCalls)
	}
}
