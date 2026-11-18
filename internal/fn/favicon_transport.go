package fn

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"

	"github.com/junfuchang/superflare/internal/netutil"
)

type siteIconOriginKey struct{}

type siteIconOrigin struct {
	address      string
	allowPrivate bool
	once         sync.Once
	ips          []netip.Addr
	err          error
}

func withSiteIconOrigin(ctx context.Context, source string) context.Context {
	u, err := url.Parse(source)
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, siteIconOriginKey{}, &siteIconOrigin{
		address:      siteIconAddress(u),
		allowPrivate: HostLooksLocalNetwork(u.Hostname()),
	})
}

func siteIconAddress(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

func validateSiteIconDestination(ctx context.Context, u *url.URL) error {
	_, err := resolveSiteIconAddresses(ctx, u)
	return err
}

func resolveSiteIconAddresses(ctx context.Context, u *url.URL) ([]string, error) {
	return resolveSiteIconAddressesWithLookup(ctx, u, lookupSiteIconIPs)
}

func resolveSiteIconAddressesWithLookup(ctx context.Context, u *url.URL, lookup func(context.Context, string) ([]netip.Addr, error)) ([]string, error) {
	origin, _ := ctx.Value(siteIconOriginKey{}).(*siteIconOrigin)
	address := siteIconAddress(u)
	isInitialOrigin := origin != nil && origin.address == address
	allowPrivate := isInitialOrigin && origin.allowPrivate
	host, port, _ := net.SplitHostPort(address)
	var ips []netip.Addr
	var err error
	if isInitialOrigin {
		// Pin the first lookup through redirects and discovery, but only explicit
		// local hostnames may use private addresses.
		origin.once.Do(func() {
			origin.ips, origin.err = lookup(ctx, host)
		})
		ips, err = origin.ips, origin.err
	} else {
		ips, err = lookup(ctx, host)
	}
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("favicon host has no addresses")
	}
	addresses := make([]string, 0, len(ips))
	for _, ip := range ips {
		ip = ip.Unmap()
		if !allowPrivate && !netutil.IsPublicIP(ip) {
			return nil, fmt.Errorf("%w: unconfigured private favicon destination", errSiteFaviconSourceNotAllowed)
		}
		addresses = append(addresses, net.JoinHostPort(ip.String(), port))
	}
	return addresses, nil
}

func lookupSiteIconIPs(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil
	}
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func safeSiteFaviconTransport() http.RoundTripper {
	return netutil.NewPinnedTransport(siteIconRequestTimeout, resolveSiteIconAddresses, netutil.ProxyFromEnvironmentOrSystem)
}
