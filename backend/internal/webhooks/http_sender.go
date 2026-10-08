package webhooks

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// Resolver resolves a hostname without giving callers a way to bypass the
// address validation performed before a network dial.
type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type HTTPSender struct {
	client *http.Client
}

func NewHTTPSender(client *http.Client) *HTTPSender {
	if client == nil {
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		transport.DialContext = func(
			ctx context.Context,
			network string,
			address string,
		) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, ErrUnsafeDestination
			}
			publicAddress, err := resolvePublicWebhookHost(
				ctx, net.DefaultResolver, host,
			)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(
				ctx, network, net.JoinHostPort(publicAddress, port),
			)
		}
		client = &http.Client{Transport: transport, Timeout: 15 * time.Second}
	}
	configured := *client
	configured.CheckRedirect = func(
		*http.Request,
		[]*http.Request,
	) error {
		return http.ErrUseLastResponse
	}
	return &HTTPSender{client: &configured}
}

func (s *HTTPSender) Send(
	ctx context.Context,
	delivery DeliveryRequest,
) (int, error) {
	if s == nil || s.client == nil || ValidateDestination(delivery.URL) != nil {
		return 0, ErrUnsafeDestination
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, delivery.URL, bytes.NewReader(delivery.Body),
	)
	if err != nil {
		return 0, err
	}
	for name, value := range delivery.Headers {
		request.Header.Set(name, value)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 32<<10))
	return response.StatusCode, nil
}

func resolvePublicWebhookHost(
	ctx context.Context,
	resolver Resolver,
	host string,
) (string, error) {
	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return "", ErrUnsafeDestination
	}
	for _, address := range addresses {
		if !IsPublicIP(address.IP) {
			return "", ErrUnsafeDestination
		}
	}
	return addresses[0].IP.String(), nil
}

// IsPublicIP preserves the net.IP boundary used by callers while delegating
// special-purpose classification to the canonical netip representation.
func IsPublicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	return ok && IsPublicAddress(address)
}

// IsPublicAddress accepts addresses that IANA marks globally reachable. It
// deliberately evaluates documented globally-reachable special allocations
// before the enclosing non-global registry blocks.
func IsPublicAddress(address netip.Addr) bool {
	if !address.IsValid() {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsLoopback() || address.IsPrivate() ||
		address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, allocation := range globallyReachableSpecialAddresses {
		if allocation.prefix.Contains(address) {
			return true
		}
	}
	for _, allocation := range nonGlobalSpecialAddresses {
		if allocation.prefix.Contains(address) {
			return false
		}
	}
	return true
}

type specialAddressAllocation struct {
	prefix netip.Prefix
	// semantic records the IANA registry's Globally Reachable result.
	semantic string
}

var globallyReachableSpecialAddresses = []specialAddressAllocation{
	{netip.MustParsePrefix("192.0.0.9/32"), "PCP anycast: Global=true"},
	{netip.MustParsePrefix("192.0.0.10/32"), "TURN anycast: Global=true"},
	{netip.MustParsePrefix("192.31.196.0/24"), "AS112-v4: Global=true"},
	{netip.MustParsePrefix("192.52.193.0/24"), "AMT: Global=true"},
	{netip.MustParsePrefix("192.175.48.0/24"), "AS112 direct delegation: Global=true"},
	{netip.MustParsePrefix("64:ff9b::/96"), "well-known NAT64 prefix: Global=true"},
	{netip.MustParsePrefix("2001:1::1/128"), "PCP anycast: Global=true"},
	{netip.MustParsePrefix("2001:1::2/128"), "TURN anycast: Global=true"},
	{netip.MustParsePrefix("2001:1::3/128"), "DNS-SD anycast: Global=true"},
	{netip.MustParsePrefix("2001:3::/32"), "AMT: Global=true"},
	{netip.MustParsePrefix("2001:4:112::/48"), "AS112-v6: Global=true"},
	{netip.MustParsePrefix("2001:20::/28"), "ORCHIDv2: Global=true"},
	{netip.MustParsePrefix("2001:30::/28"), "DETs: Global=true"},
}

var nonGlobalSpecialAddresses = []specialAddressAllocation{
	{netip.MustParsePrefix("0.0.0.0/8"), "this network: Global=false"},
	{netip.MustParsePrefix("100.64.0.0/10"), "shared address space: Global=false"},
	{netip.MustParsePrefix("192.0.0.0/24"), "IETF protocol assignments: Global=false except explicit anycasts"},
	{netip.MustParsePrefix("192.0.2.0/24"), "TEST-NET-1 documentation: Global=false"},
	{netip.MustParsePrefix("192.88.99.0/24"), "deprecated 6to4 relay: Global=false"},
	{netip.MustParsePrefix("198.18.0.0/15"), "benchmarking: Global=false"},
	{netip.MustParsePrefix("198.51.100.0/24"), "TEST-NET-2 documentation: Global=false"},
	{netip.MustParsePrefix("203.0.113.0/24"), "TEST-NET-3 documentation: Global=false"},
	{netip.MustParsePrefix("240.0.0.0/4"), "reserved IPv4: Global=false"},
	{netip.MustParsePrefix("64:ff9b:1::/48"), "local-use NAT64: Global=false"},
	{netip.MustParsePrefix("100::/64"), "discard-only: Global=false"},
	{netip.MustParsePrefix("100:0:0:1::/64"), "dummy IPv6 prefix: Global=false"},
	{netip.MustParsePrefix("2001::/23"), "IETF protocol assignments: Global=false except explicit anycasts"},
	{netip.MustParsePrefix("2001:db8::/32"), "documentation: Global=false"},
	{netip.MustParsePrefix("2002::/16"), "6to4: Global=false"},
	{netip.MustParsePrefix("3fff::/20"), "documentation: Global=false"},
	{netip.MustParsePrefix("5f00::/16"), "SRv6: Global=false"},
}
