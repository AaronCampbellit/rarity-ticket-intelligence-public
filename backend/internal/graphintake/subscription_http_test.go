package graphintake

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPSubscriptionProviderCreatesAndRenewsOfficialGraphContract(t *testing.T) {
	var (
		createdBody map[string]any
		renewedBody map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		switch {
		case request.URL.Path == "/tenant-id/oauth2/v2.0/token":
			_, _ = response.Write([]byte(
				`{"access_token":"access-token","expires_in":3600}`,
			))
		case request.URL.Path == "/v1.0/subscriptions" &&
			request.Method == http.MethodPost:
			if request.Header.Get("Authorization") != "Bearer access-token" {
				t.Fatalf("create authorization=%q", request.Header.Get("Authorization"))
			}
			if err := json.NewDecoder(request.Body).Decode(&createdBody); err != nil {
				t.Fatal(err)
			}
			response.WriteHeader(http.StatusCreated)
			_, _ = response.Write([]byte(`{
			  "id":"new-subscription-id",
			  "resource":"users/support@example.com/mailFolders('Inbox')/messages",
			  "expirationDateTime":"2026-08-05T21:00:00Z"
			}`))
		case request.URL.Path ==
			"/v1.0/subscriptions/existing-subscription-id" &&
			request.Method == http.MethodPatch:
			if err := json.NewDecoder(request.Body).Decode(&renewedBody); err != nil {
				t.Fatal(err)
			}
			_, _ = response.Write([]byte(`{
			  "id":"existing-subscription-id",
			  "resource":"users/support@example.com/mailFolders('Inbox')/messages",
			  "expirationDateTime":"2026-08-05T21:00:00Z"
			}`))
		default:
			t.Fatalf("unexpected Graph request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	factory, err := NewRuntimeSubscriptionProviderFactory(
		RuntimeSubscriptionProviderFactoryConfig{
			Client: server.Client(), GraphBaseURL: server.URL + "/v1.0",
			TokenBaseURL: server.URL,
			Credentials: &graphCredentialResolver{value: []byte(`{
			  "tenant_id":"tenant-id",
			  "client_id":"client-id",
			  "client_secret":"client-secret"
			}`)},
		},
	)
	if err != nil {
		t.Fatalf("NewRuntimeSubscriptionProviderFactory() error=%v", err)
	}
	provider, err := factory.Provider(
		context.Background(),
		SubscriptionJob{
			CredentialSecretRef: "env://RARITY_GRAPH_CREDENTIAL_SUPPORT",
		},
	)
	if err != nil {
		t.Fatalf("Provider() error=%v", err)
	}
	expiresAt := time.Date(2026, time.August, 5, 21, 0, 0, 0, time.UTC)
	request := SubscriptionRequest{
		ExternalID:               "existing-subscription-id",
		Resource:                 "users/support@example.com/mailFolders('Inbox')/messages",
		ChangeType:               "created,updated",
		NotificationURL:          "https://rarity.example/api/v1/graph/notifications",
		LifecycleNotificationURL: "https://rarity.example/api/v1/graph/notifications",
		ClientState:              "client-state", ExpiresAt: expiresAt,
	}

	created, createErr := provider.Create(context.Background(), request)
	renewed, renewErr := provider.Renew(context.Background(), request)

	if createErr != nil || renewErr != nil ||
		created.ID != "new-subscription-id" ||
		renewed.ID != "existing-subscription-id" ||
		createdBody["changeType"] != "created,updated" ||
		createdBody["clientState"] != "client-state" ||
		createdBody["lifecycleNotificationUrl"] != request.NotificationURL ||
		len(renewedBody) != 1 ||
		renewedBody["expirationDateTime"] != expiresAt.Format(time.RFC3339) {
		t.Fatalf(
			"created=%+v createErr=%v renewed=%+v renewErr=%v createBody=%+v renewBody=%+v",
			created, createErr, renewed, renewErr, createdBody, renewedBody,
		)
	}
}
