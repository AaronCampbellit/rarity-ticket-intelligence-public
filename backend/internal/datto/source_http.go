package datto

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxDattoResponseBytes = int64(16 << 20)

var (
	ErrInvalidSourceConfiguration = errors.New("invalid Datto source configuration")
	ErrInvalidSourceCredentials   = errors.New("invalid Datto source credentials")
	ErrInvalidProviderURL         = errors.New("invalid Datto provider URL")
	ErrInvalidSourceResponse      = errors.New("invalid Datto source response")
	ErrProviderRedirectRefused    = errors.New("Datto provider redirect refused")
)

type CredentialResolver interface {
	Resolve(context.Context, string) ([]byte, error)
}

type PayloadObjectStore interface {
	Put(context.Context, string, io.Reader, int64, string) error
}

type HTTPSourceConfig struct {
	Client        *http.Client
	Credentials   CredentialResolver
	CredentialRef string
	PayloadStore  PayloadObjectStore
	ObjectPrefix  string
	AllowHTTP     bool
	Now           func() time.Time
}

type HTTPSource struct {
	client        *http.Client
	credentials   CredentialResolver
	credentialRef string
	payloadStore  PayloadObjectStore
	objectPrefix  string
	allowHTTP     bool
	now           func() time.Time

	tokenMu        sync.Mutex
	apiBase        *url.URL
	accessToken    string
	tokenExpiresAt time.Time
}

var _ SyncSource = (*HTTPSource)(nil)

func NewHTTPSource(config HTTPSourceConfig) (*HTTPSource, error) {
	if config.Client == nil || config.Credentials == nil ||
		strings.TrimSpace(config.CredentialRef) == "" ||
		config.PayloadStore == nil ||
		strings.Trim(strings.TrimSpace(config.ObjectPrefix), "/") == "" {
		return nil, ErrInvalidSourceConfiguration
	}
	client := *config.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return ErrProviderRedirectRefused
	}
	if client.Timeout <= 0 {
		client.Timeout = 30 * time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &HTTPSource{
		client: &client, credentials: config.Credentials,
		credentialRef: config.CredentialRef,
		payloadStore:  config.PayloadStore,
		objectPrefix:  strings.Trim(strings.TrimSpace(config.ObjectPrefix), "/"),
		allowHTTP:     config.AllowHTTP, now: config.Now,
	}, nil
}

func (s *HTTPSource) Fetch(
	ctx context.Context,
	connection Connection,
	_ SyncKind,
) (SyncPage, error) {
	if strings.TrimSpace(connection.ID) == "" ||
		connection.CredentialSecretRef != "" &&
			connection.CredentialSecretRef != s.credentialRef {
		return SyncPage{}, ErrInvalidSourceConfiguration
	}
	if _, err := s.token(ctx); err != nil {
		return SyncPage{}, err
	}
	requestURL := s.providerURL("/api/v2/account/devices")
	parsed, _ := url.Parse(requestURL)
	query := parsed.Query()
	query.Set("max", "250")
	parsed.RawQuery = query.Encode()
	requestURL = parsed.String()

	result := SyncPage{}
	pageNumber := 1
	for requestURL != "" {
		if err := s.validateProviderURL(requestURL); err != nil {
			return SyncPage{}, err
		}
		payload, response, err := s.authorizedJSON(ctx, requestURL)
		if err != nil {
			return SyncPage{}, err
		}
		pageRef := fmt.Sprintf("%s/devices-page-%06d.json", s.objectPrefix, pageNumber)
		if err := s.storePayload(ctx, pageRef, payload); err != nil {
			return SyncPage{}, err
		}
		var page dattoDevicesPage
		if err := decodeDattoJSON(payload, &page); err != nil {
			return SyncPage{}, err
		}
		for _, device := range page.Devices {
			asset, err := s.remoteAsset(ctx, device, pageRef)
			if err != nil {
				return SyncPage{}, err
			}
			result.Assets = append(result.Assets, asset)
		}
		result.RateLimit = parseRateLimit(response.Header, s.now())
		requestURL = strings.TrimSpace(page.PageDetails.NextPageURL)
		pageNumber++
	}
	alerts, alertRateLimit, err := s.fetchAlerts(ctx)
	if err != nil {
		return SyncPage{}, err
	}
	result.Alerts = alerts
	if alertRateLimit.ResetAt.After(result.RateLimit.ResetAt) ||
		alertRateLimit.Remaining < result.RateLimit.Remaining {
		result.RateLimit = alertRateLimit
	}
	result.Changed = len(result.Assets)
	result.ObservedAt = s.now().UTC()
	result.NextCursor = result.ObservedAt.Format(time.RFC3339Nano)
	result.CompleteInventory = true
	return result, nil
}

func (s *HTTPSource) fetchAlerts(
	ctx context.Context,
) ([]AlertObservation, RateLimitState, error) {
	var (
		result    []AlertObservation
		rateLimit RateLimitState
	)
	for _, endpoint := range []struct {
		name  string
		state AlertState
	}{
		{name: "open", state: AlertActive},
		{name: "resolved", state: AlertCleared},
	} {
		requestURL := s.providerURL(
			"/api/v2/account/alerts/" + endpoint.name,
		)
		parsed, _ := url.Parse(requestURL)
		query := parsed.Query()
		query.Set("max", "250")
		parsed.RawQuery = query.Encode()
		requestURL = parsed.String()
		pageNumber := 1
		for requestURL != "" {
			payload, response, err := s.authorizedJSON(ctx, requestURL)
			if err != nil {
				return nil, rateLimit, err
			}
			pageRef := fmt.Sprintf(
				"%s/alerts-%s-page-%06d.json",
				s.objectPrefix, endpoint.name, pageNumber,
			)
			if err := s.storePayload(ctx, pageRef, payload); err != nil {
				return nil, rateLimit, err
			}
			var page dattoAlertsPage
			if err := decodeDattoJSON(payload, &page); err != nil {
				return nil, rateLimit, err
			}
			for _, alert := range page.Alerts {
				observation, err := normalizeDattoAlert(
					alert, endpoint.state, pageRef,
				)
				if err != nil {
					return nil, rateLimit, err
				}
				result = append(result, observation)
			}
			rateLimit = parseRateLimit(response.Header, s.now())
			requestURL = strings.TrimSpace(page.PageDetails.NextPageURL)
			if requestURL != "" {
				if err := s.validateProviderURL(requestURL); err != nil {
					return nil, rateLimit, err
				}
			}
			pageNumber++
		}
	}
	return result, rateLimit, nil
}

func normalizeDattoAlert(
	alert dattoAlert,
	state AlertState,
	sourcePayloadRef string,
) (AlertObservation, error) {
	observedRaw := alert.Timestamp
	if state == AlertCleared && strings.TrimSpace(alert.ResolvedOn) != "" {
		observedRaw = alert.ResolvedOn
	}
	observedAt, err := time.Parse(time.RFC3339Nano, observedRaw)
	if err != nil || strings.TrimSpace(alert.AlertUID) == "" ||
		strings.TrimSpace(alert.Source.SiteUID) == "" {
		return AlertObservation{}, ErrInvalidSourceResponse
	}
	fingerprintSource := strings.ToLower(strings.Join([]string{
		strings.TrimSpace(alert.Source.DeviceUID),
		strings.TrimSpace(alert.Context.Class),
		strings.Join(strings.Fields(alert.Diagnostics), " "),
	}, "|"))
	fingerprint := sha256.Sum256([]byte(fingerprintSource))
	title := strings.TrimSpace(alert.Diagnostics)
	if title == "" {
		title = strings.TrimSpace(alert.Context.Class)
	}
	return AlertObservation{
		ExternalID: alert.AlertUID, ExternalDeviceID: alert.Source.DeviceUID,
		SiteID: alert.Source.SiteUID, DeviceName: alert.Source.DeviceName,
		SiteName: alert.Source.SiteName, Priority: alert.Priority,
		Title: title, Diagnostics: alert.Diagnostics,
		Fingerprint: fmt.Sprintf("%x", fingerprint[:]),
		State:       state, ObservedAt: observedAt.UTC(),
		SourcePayloadRef: sourcePayloadRef,
	}, nil
}

func (s *HTTPSource) remoteAsset(
	ctx context.Context,
	device dattoDevice,
	pageRef string,
) (RemoteAsset, error) {
	if strings.TrimSpace(device.UID) == "" ||
		strings.TrimSpace(device.SiteUID) == "" {
		return RemoteAsset{}, ErrInvalidSourceResponse
	}
	auditPath := "/api/v2/audit/device/" + url.PathEscape(device.UID)
	switch device.DeviceClass {
	case "printer":
		auditPath = "/api/v2/audit/printer/" + url.PathEscape(device.UID)
	case "esxihost":
		auditPath = "/api/v2/audit/esxihost/" + url.PathEscape(device.UID)
	}
	payload, _, err := s.authorizedJSON(ctx, s.providerURL(auditPath))
	if err != nil {
		return RemoteAsset{}, err
	}
	auditRef := s.objectPrefix + "/audit/" + url.PathEscape(device.UID) + ".json"
	if err := s.storePayload(ctx, auditRef, payload); err != nil {
		return RemoteAsset{}, err
	}
	var audit dattoDeviceAudit
	if err := decodeDattoJSON(payload, &audit); err != nil {
		return RemoteAsset{}, err
	}
	macs := make([]string, 0, len(audit.NICs))
	for _, nic := range audit.NICs {
		if value := strings.TrimSpace(nic.MACAddress); value != "" {
			macs = append(macs, value)
		}
	}
	updatedAt, _ := time.Parse(time.RFC3339Nano, device.LastAuditDate)
	return RemoteAsset{
		ExternalID: device.UID, SiteID: device.SiteUID,
		Hostname: device.Hostname, SerialNumber: audit.BIOS.SerialNumber,
		MACAddresses: macs, SourcePayloadRef: pageRef,
		SourceUpdatedAt: updatedAt.UTC(),
	}, nil
}

func (s *HTTPSource) authorizedJSON(
	ctx context.Context,
	requestURL string,
) ([]byte, *http.Response, error) {
	if err := s.validateProviderURL(requestURL); err != nil {
		return nil, nil, err
	}
	token, err := s.token(ctx)
	if err != nil {
		return nil, nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		request, err := http.NewRequestWithContext(
			ctx, http.MethodGet, requestURL, nil,
		)
		if err != nil {
			return nil, nil, ErrInvalidProviderURL
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Accept", "application/json")
		response, err := s.client.Do(request)
		if err != nil {
			return nil, nil, err
		}
		if response.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			_ = response.Body.Close()
			if err := waitForDattoRetry(
				ctx, response.Header.Get("Retry-After"), s.now(),
			); err != nil {
				return nil, response, err
			}
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_ = response.Body.Close()
			return nil, response, ErrInvalidSourceResponse
		}
		payload, err := io.ReadAll(io.LimitReader(
			response.Body, maxDattoResponseBytes+1,
		))
		_ = response.Body.Close()
		if err != nil || int64(len(payload)) > maxDattoResponseBytes {
			return nil, response, ErrInvalidSourceResponse
		}
		return payload, response, nil
	}
	return nil, nil, ErrInvalidSourceResponse
}

func waitForDattoRetry(
	ctx context.Context,
	raw string,
	now time.Time,
) error {
	delay := time.Second
	if seconds, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
		delay = time.Duration(seconds) * time.Second
	} else if at, err := http.ParseTime(raw); err == nil {
		delay = at.Sub(now)
	}
	if delay < 0 {
		delay = 0
	}
	if delay > time.Minute {
		delay = time.Minute
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *HTTPSource) token(ctx context.Context) (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	if s.accessToken != "" &&
		s.now().Before(s.tokenExpiresAt.Add(-5*time.Minute)) {
		return s.accessToken, nil
	}
	protected, err := s.credentials.Resolve(ctx, s.credentialRef)
	if err != nil {
		return "", err
	}
	var credential struct {
		APIURL    string `json:"api_url"`
		APIKey    string `json:"api_key"`
		APISecret string `json:"api_secret"`
	}
	decoder := json.NewDecoder(bytes.NewReader(protected))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&credential); err != nil ||
		strings.TrimSpace(credential.APIKey) == "" ||
		strings.TrimSpace(credential.APISecret) == "" {
		return "", ErrInvalidSourceCredentials
	}
	base, err := url.Parse(strings.TrimSpace(credential.APIURL))
	if err != nil || !base.IsAbs() || base.User != nil || base.Host == "" ||
		base.RawQuery != "" || base.Fragment != "" ||
		(base.Scheme != "https" && !(s.allowHTTP && base.Scheme == "http")) ||
		!s.allowHTTP && !strings.HasSuffix(
			strings.ToLower(base.Hostname()), ".centrastage.net",
		) {
		return "", ErrInvalidProviderURL
	}
	base.Path = strings.TrimSuffix(base.Path, "/")
	s.apiBase = base
	form := url.Values{
		"grant_type": {"password"},
		"username":   {credential.APIKey},
		"password":   {credential.APISecret},
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, s.providerURL("/auth/oauth/token"),
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", ErrInvalidProviderURL
	}
	request.SetBasicAuth("public-client", "public")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", ErrInvalidSourceCredentials
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(
		&token,
	); err != nil || strings.TrimSpace(token.AccessToken) == "" ||
		token.ExpiresIn <= 0 {
		return "", ErrInvalidSourceCredentials
	}
	s.accessToken = token.AccessToken
	s.tokenExpiresAt = s.now().Add(
		time.Duration(token.ExpiresIn) * time.Second,
	)
	return s.accessToken, nil
}

func (s *HTTPSource) validateProviderURL(raw string) error {
	if s.apiBase == nil {
		return ErrInvalidProviderURL
	}
	value, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !value.IsAbs() || value.User != nil ||
		value.Scheme != s.apiBase.Scheme ||
		!strings.EqualFold(value.Host, s.apiBase.Host) ||
		!strings.HasPrefix(path.Clean(value.EscapedPath()), "/api/") {
		return ErrInvalidProviderURL
	}
	return nil
}

func (s *HTTPSource) providerURL(resourcePath string) string {
	if s.apiBase == nil {
		return ""
	}
	value := *s.apiBase
	value.Path = strings.TrimSuffix(value.Path, "/") + resourcePath
	value.RawPath = ""
	return value.String()
}

func (s *HTTPSource) storePayload(
	ctx context.Context,
	key string,
	payload []byte,
) error {
	return s.payloadStore.Put(
		ctx, key, bytes.NewReader(payload), int64(len(payload)),
		"application/json",
	)
}

type dattoDevicesPage struct {
	PageDetails struct {
		NextPageURL string `json:"nextPageUrl"`
	} `json:"pageDetails"`
	Devices []dattoDevice `json:"devices"`
}

type dattoDevice struct {
	UID           string `json:"uid"`
	SiteUID       string `json:"siteUid"`
	Hostname      string `json:"hostname"`
	DeviceClass   string `json:"deviceClass"`
	LastAuditDate string `json:"lastAuditDate"`
}

type dattoDeviceAudit struct {
	BIOS struct {
		SerialNumber string `json:"serialNumber"`
	} `json:"bios"`
	NICs []struct {
		MACAddress string `json:"macAddress"`
	} `json:"nics"`
}

type dattoAlertsPage struct {
	PageDetails struct {
		NextPageURL string `json:"nextPageUrl"`
	} `json:"pageDetails"`
	Alerts []dattoAlert `json:"alerts"`
}

type dattoAlert struct {
	AlertUID    string `json:"alertUid"`
	Priority    string `json:"priority"`
	Diagnostics string `json:"diagnostics"`
	Timestamp   string `json:"timestamp"`
	ResolvedOn  string `json:"resolvedOn"`
	Context     struct {
		Class string `json:"@class"`
	} `json:"alertContext"`
	Source struct {
		DeviceUID  string `json:"deviceUid"`
		DeviceName string `json:"deviceName"`
		SiteUID    string `json:"siteUid"`
		SiteName   string `json:"siteName"`
	} `json:"alertSourceInfo"`
}

func decodeDattoJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(target); err != nil {
		return ErrInvalidSourceResponse
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidSourceResponse
	}
	return nil
}

func parseRateLimit(header http.Header, now time.Time) RateLimitState {
	remaining, _ := strconv.Atoi(header.Get("X-RateLimit-Remaining"))
	resetSeconds, _ := strconv.ParseInt(
		header.Get("X-RateLimit-Reset"), 10, 64,
	)
	result := RateLimitState{Remaining: remaining}
	if resetSeconds > 0 {
		result.ResetAt = time.Unix(resetSeconds, 0).UTC()
	} else {
		result.ResetAt = now.UTC().Add(time.Minute)
	}
	return result
}
