package editor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/junfuchang/superflare/internal/fn"
	"github.com/junfuchang/superflare/internal/netutil"
)

var errLinkCheckDestinationNotAllowed = errors.New("link checks only allow public HTTP(S) destinations")

func isPublicLinkCheckIP(ip netip.Addr) bool {
	return netutil.IsPublicIP(ip)
}

func validateLinkCheckURL(u *url.URL) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return errLinkCheckDestinationNotAllowed
	}
	host := strings.TrimSuffix(u.Hostname(), ".")
	if ip, err := netip.ParseAddr(host); err == nil {
		if !isPublicLinkCheckIP(ip) {
			return errLinkCheckDestinationNotAllowed
		}
	} else if fn.HostLooksLocalNetwork(host) {
		return errLinkCheckDestinationNotAllowed
	}
	return nil
}

type linkCheckDialTarget struct {
	addresses []string
	proxyURL  *url.URL
}

type linkCheckDialKey struct{}

type linkCheckTransport struct {
	transport *http.Transport
	lookup    func(context.Context, string, string) ([]netip.Addr, error)
	proxy     func(*http.Request) (*url.URL, error)
}

func newLinkCheckTransport() *linkCheckTransport {
	return &linkCheckTransport{
		lookup: net.DefaultResolver.LookupNetIP,
		proxy:  netutil.ProxyFromCurrentEnvironment,
		transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				target, ok := ctx.Value(linkCheckDialKey{}).(linkCheckDialTarget)
				if !ok {
					return nil, errLinkCheckDestinationNotAllowed
				}
				return netutil.DialPinnedContext(ctx, linkCheckRequestTimeout, network, target.addresses, target.proxyURL)
			},
			DisableKeepAlives:      true,
			TLSHandshakeTimeout:    linkCheckRequestTimeout,
			ResponseHeaderTimeout:  linkCheckRequestTimeout,
			MaxResponseHeaderBytes: 1 << 20,
		},
	}
}

func (t *linkCheckTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := validateLinkCheckURL(req.URL); err != nil {
		return nil, err
	}
	host := strings.TrimSuffix(req.URL.Hostname(), ".")
	ips := []netip.Addr{}
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = append(ips, ip)
	} else {
		var err error
		ips, err = t.lookup(req.Context(), "ip", host)
		if err != nil {
			return nil, err
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("link check host has no addresses")
	}
	port := req.URL.Port()
	if port == "" {
		port = "80"
		if req.URL.Scheme == "https" {
			port = "443"
		}
	}
	target := linkCheckDialTarget{}
	for _, ip := range ips {
		if !isPublicLinkCheckIP(ip) {
			return nil, errLinkCheckDestinationNotAllowed
		}
		target.addresses = append(target.addresses, net.JoinHostPort(ip.Unmap().String(), port))
	}
	var err error
	target.proxyURL, err = t.proxy(req)
	if err != nil {
		return nil, err
	}
	// Pin validated IPs for both direct and proxied connections. Keep the
	// original URL so HTTP Host, TLS verification and redirects use the hostname.
	ctx := context.WithValue(req.Context(), linkCheckDialKey{}, target)
	return t.transport.RoundTrip(req.WithContext(ctx))
}

func (t *linkCheckTransport) CloseIdleConnections() {
	t.transport.CloseIdleConnections()
}

func newLinkCheckClient() *http.Client {
	return &http.Client{
		Timeout:   linkCheckRequestTimeout,
		Transport: newLinkCheckTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after too many link check redirects")
			}
			return validateLinkCheckURL(req.URL)
		},
	}
}
