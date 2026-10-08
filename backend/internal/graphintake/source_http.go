package graphintake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

const maxGraphResponseBytes = int64(64 << 20)

var (
	ErrInvalidGraphConfiguration = errors.New("invalid Graph source configuration")
	ErrInvalidGraphCredentials   = errors.New("invalid Graph credentials")
	ErrInvalidGraphCursor        = errors.New("invalid Graph delta cursor")
	ErrInvalidGraphResponse      = errors.New("invalid Graph response")
)

type GraphCredentialResolver interface {
	Resolve(context.Context, string) ([]byte, error)
}

type MIMEObjectStore interface {
	Put(context.Context, string, io.Reader, int64, string) error
}

type HTTPSourceConfig struct {
	Client              *http.Client
	GraphBaseURL        string
	TokenBaseURL        string
	CredentialRef       string
	Credentials         GraphCredentialResolver
	MIMEStore           MIMEObjectStore
	MIMEObjectKeyPrefix string
}

type HTTPSource struct {
	client              *http.Client
	graphBase           *url.URL
	tokenBase           *url.URL
	credentialRef       string
	credentials         GraphCredentialResolver
	mimeStore           MIMEObjectStore
	mimeObjectKeyPrefix string

	tokenMu        sync.Mutex
	accessToken    string
	tokenExpiresAt time.Time
}

var _ GraphSource = (*HTTPSource)(nil)

func NewHTTPSource(config HTTPSourceConfig) (*HTTPSource, error) {
	graphBase, graphErr := parseServiceURL(config.GraphBaseURL)
	tokenBase, tokenErr := parseServiceURL(config.TokenBaseURL)
	if config.Client == nil || graphErr != nil || tokenErr != nil ||
		strings.TrimSpace(config.CredentialRef) == "" ||
		config.Credentials == nil || config.MIMEStore == nil ||
		strings.Trim(strings.TrimSpace(config.MIMEObjectKeyPrefix), "/") == "" {
		return nil, ErrInvalidGraphConfiguration
	}
	return &HTTPSource{
		client: config.Client, graphBase: graphBase, tokenBase: tokenBase,
		credentialRef: config.CredentialRef, credentials: config.Credentials,
		mimeStore: config.MIMEStore,
		mimeObjectKeyPrefix: strings.Trim(
			strings.TrimSpace(config.MIMEObjectKeyPrefix), "/",
		),
	}, nil
}

func (s *HTTPSource) FetchDelta(
	ctx context.Context,
	mailbox string,
	folder string,
	cursor string,
) (DeltaPage, error) {
	requestURL, err := s.deltaURL(mailbox, folder, cursor)
	if err != nil {
		return DeltaPage{}, err
	}
	var response graphDeltaResponse
	if err := s.getJSON(ctx, requestURL, &response); err != nil {
		return DeltaPage{}, err
	}
	nextCursor := response.DeltaLink
	if strings.TrimSpace(response.NextLink) != "" {
		nextCursor = response.NextLink
	}
	if err := s.validateCursor(nextCursor); err != nil {
		return DeltaPage{}, err
	}
	messages := make([]Message, 0, len(response.Value))
	for _, item := range response.Value {
		message, err := item.message()
		if err != nil {
			return DeltaPage{}, err
		}
		rawRef, err := s.retrieveMIME(ctx, mailbox, message.ID)
		if err != nil {
			return DeltaPage{}, err
		}
		message.RawMIMERef = rawRef
		messages = append(messages, message)
	}
	return DeltaPage{Messages: messages, NextCursor: nextCursor}, nil
}

func (s *HTTPSource) GetMessage(
	ctx context.Context,
	mailbox string,
	messageID string,
) (Message, error) {
	requestURL, err := s.messageURL(mailbox, messageID, "")
	if err != nil {
		return Message{}, err
	}
	var item graphMessage
	if err := s.getJSON(ctx, requestURL, &item); err != nil {
		return Message{}, err
	}
	message, err := item.message()
	if err != nil || message.ID != strings.TrimSpace(messageID) {
		return Message{}, ErrInvalidGraphResponse
	}
	message.RawMIMERef, err = s.retrieveMIME(ctx, mailbox, message.ID)
	if err != nil {
		return Message{}, err
	}
	return message, nil
}

func (s *HTTPSource) deltaURL(mailbox, folder, cursor string) (string, error) {
	if value := strings.TrimSpace(cursor); value != "" {
		if err := s.validateCursor(value); err != nil {
			return "", err
		}
		return value, nil
	}
	if strings.TrimSpace(mailbox) == "" || strings.TrimSpace(folder) == "" {
		return "", ErrInvalidGraphConfiguration
	}
	return s.resourceURL(
		"users", mailbox, "mailFolders", folder, "messages", "delta",
	), nil
}

func (s *HTTPSource) messageURL(
	mailbox string,
	messageID string,
	suffix string,
) (string, error) {
	if strings.TrimSpace(mailbox) == "" || strings.TrimSpace(messageID) == "" {
		return "", ErrInvalidGraphConfiguration
	}
	segments := []string{"users", mailbox, "messages", messageID}
	if suffix != "" {
		segments = append(segments, suffix)
	}
	return s.resourceURL(segments...), nil
}

func (s *HTTPSource) resourceURL(segments ...string) string {
	result := *s.graphBase
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		escaped = append(escaped, url.PathEscape(strings.TrimSpace(segment)))
	}
	result.Path = strings.TrimSuffix(s.graphBase.Path, "/") + "/" +
		strings.Join(escaped, "/")
	result.RawPath = result.Path
	return result.String()
}

func (s *HTTPSource) validateCursor(raw string) error {
	cursor, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !cursor.IsAbs() || cursor.User != nil ||
		cursor.Scheme != s.graphBase.Scheme ||
		!strings.EqualFold(cursor.Host, s.graphBase.Host) ||
		!strings.HasPrefix(
			path.Clean(cursor.EscapedPath())+"/",
			path.Clean(s.graphBase.EscapedPath())+"/",
		) {
		return ErrInvalidGraphCursor
	}
	return nil
}

func (s *HTTPSource) retrieveMIME(
	ctx context.Context,
	mailbox string,
	messageID string,
) (string, error) {
	requestURL, err := s.messageURL(mailbox, messageID, "$value")
	if err != nil {
		return "", err
	}
	response, err := s.authorizedGET(ctx, requestURL)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", ErrInvalidGraphResponse
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxGraphResponseBytes+1))
	if err != nil || int64(len(payload)) > maxGraphResponseBytes {
		return "", ErrInvalidGraphResponse
	}
	key := s.mimeObjectKeyPrefix + "/" + url.PathEscape(messageID) + ".eml"
	if err := s.mimeStore.Put(
		ctx, key, bytes.NewReader(payload), int64(len(payload)), "message/rfc822",
	); err != nil {
		return "", err
	}
	return key, nil
}

func (s *HTTPSource) getJSON(
	ctx context.Context,
	requestURL string,
	target any,
) error {
	response, err := s.authorizedGET(ctx, requestURL)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ErrInvalidGraphResponse
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxGraphResponseBytes+1))
	if err := decoder.Decode(target); err != nil {
		return ErrInvalidGraphResponse
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidGraphResponse
	}
	return nil
}

func (s *HTTPSource) authorizedGET(
	ctx context.Context,
	requestURL string,
) (*http.Response, error) {
	token, err := s.token(ctx)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, ErrInvalidGraphConfiguration
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	return response, nil
}

type graphCredentials struct {
	TenantID     string `json:"tenant_id"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func (s *HTTPSource) token(ctx context.Context) (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	if s.accessToken != "" && time.Now().Before(s.tokenExpiresAt.Add(-time.Minute)) {
		return s.accessToken, nil
	}
	protected, err := s.credentials.Resolve(ctx, s.credentialRef)
	if err != nil {
		return "", err
	}
	var credential graphCredentials
	decoder := json.NewDecoder(bytes.NewReader(protected))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&credential); err != nil ||
		strings.TrimSpace(credential.TenantID) == "" ||
		strings.TrimSpace(credential.ClientID) == "" ||
		strings.TrimSpace(credential.ClientSecret) == "" {
		return "", ErrInvalidGraphCredentials
	}
	tokenURL := *s.tokenBase
	if !strings.HasSuffix(tokenURL.Path, "/oauth2/v2.0/token") {
		tokenURL.Path = strings.TrimSuffix(tokenURL.Path, "/") + "/" +
			url.PathEscape(credential.TenantID) + "/oauth2/v2.0/token"
	}
	form := url.Values{
		"client_id":     {credential.ClientID},
		"client_secret": {credential.ClientSecret},
		"grant_type":    {"client_credentials"},
		"scope":         {"https://graph.microsoft.com/.default"},
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, tokenURL.String(),
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", ErrInvalidGraphConfiguration
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", ErrInvalidGraphCredentials
	}
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(
		io.LimitReader(response.Body, 1<<20),
	).Decode(&result); err != nil ||
		strings.TrimSpace(result.AccessToken) == "" ||
		result.ExpiresIn <= 0 {
		return "", ErrInvalidGraphCredentials
	}
	s.accessToken = result.AccessToken
	s.tokenExpiresAt = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	return s.accessToken, nil
}

type graphDeltaResponse struct {
	Value     []graphMessage `json:"value"`
	NextLink  string         `json:"@odata.nextLink"`
	DeltaLink string         `json:"@odata.deltaLink"`
}

type graphMessage struct {
	ID                string `json:"id"`
	ConversationID    string `json:"conversationId"`
	InternetMessageID string `json:"internetMessageId"`
	Subject           string `json:"subject"`
	ReceivedDateTime  string `json:"receivedDateTime"`
	From              struct {
		EmailAddress struct {
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"from"`
	InternetMessageHeaders []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"internetMessageHeaders"`
}

func (m graphMessage) message() (Message, error) {
	receivedAt, err := time.Parse(time.RFC3339Nano, m.ReceivedDateTime)
	if err != nil || strings.TrimSpace(m.ID) == "" ||
		strings.TrimSpace(m.From.EmailAddress.Address) == "" {
		return Message{}, ErrInvalidGraphResponse
	}
	result := Message{
		ID: m.ID, ConversationID: m.ConversationID,
		InternetMessageID: m.InternetMessageID, Subject: m.Subject,
		Sender: m.From.EmailAddress.Address, ReceivedAt: receivedAt.UTC(),
	}
	for _, header := range m.InternetMessageHeaders {
		switch strings.ToLower(strings.TrimSpace(header.Name)) {
		case "in-reply-to":
			result.InReplyTo = strings.TrimSpace(header.Value)
		case "references":
			result.References = strings.Fields(header.Value)
		}
	}
	return result, nil
}

func parseServiceURL(raw string) (*url.URL, error) {
	value, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !value.IsAbs() || value.User != nil ||
		(value.Scheme != "https" && value.Scheme != "http") ||
		value.Host == "" || value.RawQuery != "" || value.Fragment != "" {
		return nil, fmt.Errorf("invalid service URL")
	}
	return value, nil
}
