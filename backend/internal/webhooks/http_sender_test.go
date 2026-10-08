package webhooks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"testing"
)

type webhookRoundTripper struct {
	request *http.Request
	body    []byte
}

func (r *webhookRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	r.request = request
	r.body, _ = io.ReadAll(request.Body)
	return &http.Response{
		StatusCode: http.StatusAccepted,
		Body:       io.NopCloser(bytes.NewBufferString("accepted")),
		Header:     make(http.Header),
	}, nil
}

func TestHTTPSenderPostsExactBodyAndHeaders(t *testing.T) {
	transport := &webhookRoundTripper{}
	sender := NewHTTPSender(&http.Client{Transport: transport})
	status, err := sender.Send(context.Background(), DeliveryRequest{
		URL: "https://hooks.example.com/rarity",
		Headers: map[string]string{
			"Content-Type":       "application/json",
			"X-Rarity-Event-ID":  "event",
			"X-Rarity-Signature": "v1=signature",
		},
		Body: []byte(`{"event":"created"}`),
	})
	if err != nil || status != http.StatusAccepted ||
		transport.request.Method != http.MethodPost ||
		transport.request.Header.Get("X-Rarity-Signature") != "v1=signature" ||
		!bytes.Equal(transport.body, []byte(`{"event":"created"}`)) {
		t.Fatalf("Send() status=%d error=%v request=%+v body=%s", status, err, transport.request, transport.body)
	}
}

func TestDefaultHTTPSenderDisablesProxyBypass(t *testing.T) {
	sender := NewHTTPSender(nil)
	transport, ok := sender.client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatalf("default transport can bypass validated DNS through a proxy: %#v", sender.client.Transport)
	}
}

type webhookResolver struct {
	addresses []net.IPAddr
}

func (r webhookResolver) LookupIPAddr(
	context.Context,
	string,
) ([]net.IPAddr, error) {
	return r.addresses, nil
}

func TestResolvePublicWebhookHostRejectsAnyPrivateDNSAnswer(t *testing.T) {
	for _, addresses := range [][]net.IPAddr{
		{{IP: net.ParseIP("127.0.0.1")}},
		{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("10.0.0.4")}},
		{{IP: net.ParseIP("169.254.169.254")}},
	} {
		if _, err := resolvePublicWebhookHost(
			context.Background(), webhookResolver{addresses: addresses}, "hooks.example.com",
		); !errors.Is(err, ErrUnsafeDestination) {
			t.Fatalf("addresses=%v error=%v", addresses, err)
		}
	}
	address, err := resolvePublicWebhookHost(
		context.Background(),
		webhookResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}},
		"hooks.example.com",
	)
	if err != nil || address != "8.8.8.8" {
		t.Fatalf("public address=%q error=%v", address, err)
	}
}

func TestIsPublicAddressAppliesIANAReachabilitySemantics(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{"ordinary IPv4", "8.8.8.8", true},
		{"ordinary IPv6", "2606:4700:4700::1111", true},
		{"private", "10.0.0.1", false},
		{"loopback", "127.0.0.1", false},
		{"link local", "169.254.1.1", false},
		{"IPv6 link local", "fe80::1", false},
		{"IPv6 unique local", "fc00::1", false},
		{"IPv6 multicast", "ff02::1", false},
		{"carrier grade NAT", "100.64.0.1", false},
		{"documentation IPv4", "203.0.113.1", false},
		{"benchmark", "198.18.0.1", false},
		{"documentation IPv6", "2001:db8::1", false},
		{"dummy IPv6", "100:0:0:1::1", false},
		{"mapped private", "::ffff:10.0.0.1", false},
		{"mapped carrier grade NAT", "::ffff:100.64.0.1", false},
		{"mapped public", "::ffff:8.8.8.8", true},
		{"well-known NAT64", "64:ff9b::c000:201", true},
		{"ORCHIDv2", "2001:20::1", true},
		{"DETs", "2001:30::1", true},
		{"local-use NAT64", "64:ff9b:1::1", false},
		{"PCP anycast", "192.0.0.9", true},
		{"AS112 IPv4", "192.31.196.1", true},
		{"AMT anycast IPv4", "192.52.193.1", true},
		{"AS112 direct delegation", "192.175.48.1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			address, err := netip.ParseAddr(tc.raw)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.raw, err)
			}
			if got := IsPublicAddress(address); got != tc.want {
				t.Fatalf("IsPublicAddress(%s)=%t, want %t", tc.raw, got, tc.want)
			}
		})
	}
}

func TestIsPublicIPUnmapsMappedCarrierGradeNAT(t *testing.T) {
	if IsPublicIP(net.ParseIP("::ffff:100.64.0.1")) {
		t.Fatal("mapped carrier-grade NAT address accepted")
	}
}

func TestResolvePublicWebhookHostUsesSharedReachabilityValidator(t *testing.T) {
	allowed, err := resolvePublicWebhookHost(context.Background(), webhookResolver{addresses: []net.IPAddr{{IP: net.ParseIP("64:ff9b::c000:201")}}}, "hooks.example.com")
	if err != nil || allowed != "64:ff9b::c000:201" {
		t.Fatalf("globally reachable NAT64 address=%q error=%v", allowed, err)
	}
	if _, err := resolvePublicWebhookHost(context.Background(), webhookResolver{addresses: []net.IPAddr{{IP: net.ParseIP("100:0:0:1::1")}}}, "hooks.example.com"); !errors.Is(err, ErrUnsafeDestination) {
		t.Fatalf("non-global dummy IPv6 error=%v", err)
	}
}
