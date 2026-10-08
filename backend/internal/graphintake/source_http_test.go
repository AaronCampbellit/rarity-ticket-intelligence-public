package graphintake

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type graphRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn graphRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type graphCredentialResolver struct {
	ref   string
	value []byte
}

func (r *graphCredentialResolver) Resolve(
	_ context.Context,
	ref string,
) ([]byte, error) {
	r.ref = ref
	return append([]byte(nil), r.value...), nil
}

type graphMIMEStore struct {
	keys    []string
	payload [][]byte
}

func (s *graphMIMEStore) Put(
	_ context.Context,
	key string,
	body io.Reader,
	size int64,
	contentType string,
) error {
	value, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if int64(len(value)) != size || contentType != "message/rfc822" {
		return ErrInvalidGraphResponse
	}
	s.keys = append(s.keys, key)
	s.payload = append(s.payload, value)
	return nil
}

func TestHTTPSourceFetchDeltaRetrievesExactMIMEAndReturnsOpaqueCursor(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		if request.Method != http.MethodPost {
			t.Fatalf("token method = %s", request.Method)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if request.Form.Get("client_id") != "client-id" ||
			request.Form.Get("client_secret") != "synthetic-secret" ||
			request.Form.Get("scope") != "https://graph.microsoft.com/.default" {
			t.Fatalf("unexpected token form: %v", request.Form)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"access_token":"access-token","expires_in":3600}`))
	}))
	defer tokenServer.Close()

	graphServer := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		if request.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/v1.0/users/support@example.com/mailFolders/inbox/messages/delta":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
			  "@odata.context":"https://graph.microsoft.com/v1.0/$metadata#messages",
			  "value":[{
			    "id":"message-id",
			    "hasAttachments":false,
			    "conversationId":"conversation-id",
			    "internetMessageId":"<message@example.com>",
			    "subject":"Printer unavailable",
			    "receivedDateTime":"2026-07-29T19:00:00Z",
			    "from":{"emailAddress":{"address":"sender@example.net"}},
			    "internetMessageHeaders":[
			      {"name":"In-Reply-To","value":"<parent@example.com>"},
			      {"name":"References","value":"<first@example.com> <second@example.com>"}
			    ]
			  }],
			  "@odata.deltaLink":"` + graphServerURL(request) + `/v1.0/delta?token=opaque"
			}`))
		case "/v1.0/users/support@example.com/messages/message-id/$value":
			response.Header().Set("Content-Type", "message/rfc822")
			_, _ = response.Write([]byte("From: sender@example.net\r\n\r\nExact body"))
		default:
			t.Fatalf("unexpected Graph path: %s", request.URL.EscapedPath())
		}
	}))
	defer graphServer.Close()

	resolver := &graphCredentialResolver{value: []byte(`{
	  "tenant_id":"tenant-id",
	  "client_id":"client-id",
	  "client_secret":"synthetic-secret"
	}`)}
	mimeStore := &graphMIMEStore{}
	source, err := NewHTTPSource(HTTPSourceConfig{
		Client:              graphServer.Client(),
		GraphBaseURL:        graphServer.URL + "/v1.0",
		TokenBaseURL:        tokenServer.URL,
		CredentialRef:       "env://RARITY_GRAPH_CREDENTIAL_SUPPORT",
		Credentials:         resolver,
		MIMEStore:           mimeStore,
		MIMEObjectKeyPrefix: "graph/connection-id",
	})
	if err != nil {
		t.Fatalf("NewHTTPSource() error = %v", err)
	}

	page, err := source.FetchDelta(
		context.Background(), "support@example.com", "inbox", "",
	)
	if err != nil {
		t.Fatalf("FetchDelta() error = %v", err)
	}
	if resolver.ref != "env://RARITY_GRAPH_CREDENTIAL_SUPPORT" ||
		len(page.Messages) != 1 ||
		page.Messages[0].RawMIMERef != "graph/connection-id/message-id.eml" ||
		page.Messages[0].InReplyTo != "<parent@example.com>" ||
		strings.Join(page.Messages[0].References, "|") !=
			"<first@example.com>|<second@example.com>" ||
		!strings.Contains(page.NextCursor, "token=opaque") {
		t.Fatalf("unexpected delta page: %+v resolver=%q", page, resolver.ref)
	}
	if len(mimeStore.payload) != 1 ||
		!bytes.Equal(mimeStore.payload[0], []byte("From: sender@example.net\r\n\r\nExact body")) {
		t.Fatalf("MIME was not stored exactly: %#v", mimeStore.payload)
	}
}

func TestHTTPSourceRejectsCursorOutsideConfiguredGraphOrigin(t *testing.T) {
	source, err := NewHTTPSource(HTTPSourceConfig{
		Client:        http.DefaultClient,
		GraphBaseURL:  "https://graph.microsoft.com/v1.0",
		TokenBaseURL:  "https://login.microsoftonline.com",
		CredentialRef: "env://RARITY_GRAPH_CREDENTIAL_SUPPORT",
		Credentials: &graphCredentialResolver{value: []byte(`{
		  "tenant_id":"tenant-id",
		  "client_id":"client-id",
		  "client_secret":"synthetic-secret"
		}`)},
		MIMEStore:           &graphMIMEStore{},
		MIMEObjectKeyPrefix: "graph/connection-id",
	})
	if err != nil {
		t.Fatalf("NewHTTPSource() error = %v", err)
	}

	_, err = source.FetchDelta(
		context.Background(),
		"support@example.com",
		"inbox",
		"https://attacker.example/v1.0/messages/delta?token=stolen",
	)
	if err != ErrInvalidGraphCursor {
		t.Fatalf("FetchDelta() error = %v, want ErrInvalidGraphCursor", err)
	}
}

func graphServerURL(request *http.Request) string {
	return "http://" + request.Host
}
