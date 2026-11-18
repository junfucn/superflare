package netutil

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

type pinnedDialKey struct{}

type pinnedDialTarget struct {
	addresses []string
	proxyURL  *url.URL
}

// PinnedTransport resolves and validates each request before a direct
// connection or proxy request. Resolve must return approved IP:port addresses.
type PinnedTransport struct {
	Transport *http.Transport
	Timeout   time.Duration
	Resolve   func(context.Context, *url.URL) ([]string, error)
	Proxy     func(*http.Request) (*url.URL, error)
}

func NewPinnedTransport(timeout time.Duration, resolve func(context.Context, *url.URL) ([]string, error), proxyForRequest func(*http.Request) (*url.URL, error)) *PinnedTransport {
	t := &PinnedTransport{
		Timeout: timeout,
		Resolve: resolve,
		Proxy:   proxyForRequest,
		Transport: &http.Transport{
			// A pooled connection must not outlive the request's destination policy.
			DisableKeepAlives:      true,
			TLSHandshakeTimeout:    timeout,
			ResponseHeaderTimeout:  timeout,
			MaxResponseHeaderBytes: 1 << 20,
		},
	}
	t.Transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		target, ok := ctx.Value(pinnedDialKey{}).(pinnedDialTarget)
		if !ok {
			return nil, fmt.Errorf("missing validated destination")
		}
		return dialPinnedContext(ctx, timeout, network, target.addresses, target.proxyURL, t.Transport.TLSClientConfig)
	}
	return t
}

func (t *PinnedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.User != nil {
		return nil, fmt.Errorf("unsupported image destination")
	}
	addresses, err := t.Resolve(req.Context(), req.URL)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("destination has no addresses")
	}
	for _, address := range addresses {
		if _, err := netip.ParseAddrPort(address); err != nil {
			return nil, fmt.Errorf("destination is not pinned to an IP")
		}
	}
	var proxyURL *url.URL
	if t.Proxy != nil {
		proxyURL, err = t.Proxy(req)
		if err != nil {
			return nil, err
		}
	}
	// Tunnel HTTP requests too: forward proxies replace Host with the absolute URI's IP.
	ctx := context.WithValue(req.Context(), pinnedDialKey{}, pinnedDialTarget{addresses: addresses, proxyURL: proxyURL})
	return t.Transport.RoundTrip(req.WithContext(ctx))
}

func (t *PinnedTransport) CloseIdleConnections() {
	t.Transport.CloseIdleConnections()
}

// DialPinnedContext divides the remaining connection budget among approved
// IP:port addresses, including the time spent establishing proxy tunnels.
func DialPinnedContext(ctx context.Context, timeout time.Duration, network string, addresses []string, proxyURL *url.URL) (net.Conn, error) {
	return dialPinnedContext(ctx, timeout, network, addresses, proxyURL, nil)
}

func dialPinnedContext(ctx context.Context, timeout time.Duration, network string, addresses []string, proxyURL *url.URL, tlsConfig *tls.Config) (net.Conn, error) {
	if len(addresses) == 0 {
		return nil, fmt.Errorf("destination has no addresses")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	dialer := &net.Dialer{Timeout: timeout}
	var dialErr error
	for index, address := range addresses {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(dialErr, err)
		}
		attemptTimeout := time.Until(deadline) / time.Duration(len(addresses)-index)
		attemptCtx, cancelAttempt := context.WithTimeout(ctx, attemptTimeout)
		conn, err := dialProxyContext(attemptCtx, dialer, network, address, proxyURL, tlsConfig)
		cancelAttempt()
		if err == nil {
			return conn, nil
		}
		dialErr = errors.Join(dialErr, err)
	}
	return nil, errors.Join(dialErr, ctx.Err())
}

// DialProxyContext preserves the approved IP through HTTP CONNECT and SOCKS.
// The caller keeps the original request URL for Host and TLS verification.
func DialProxyContext(ctx context.Context, dialer *net.Dialer, network, address string, proxyURL *url.URL) (net.Conn, error) {
	return dialProxyContext(ctx, dialer, network, address, proxyURL, nil)
}

func dialProxyContext(ctx context.Context, dialer *net.Dialer, network, address string, proxyURL *url.URL, tlsConfig *tls.Config) (net.Conn, error) {
	if proxyURL == nil {
		return dialer.DialContext(ctx, network, address)
	}
	if proxyURL.Scheme == "socks5" || proxyURL.Scheme == "socks5h" {
		proxyDialer, err := proxy.FromURL(proxyURL, dialer)
		if err != nil {
			return nil, err
		}
		return proxyDialer.(proxy.ContextDialer).DialContext(ctx, network, address)
	}
	if proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
		return nil, fmt.Errorf("unsupported proxy scheme")
	}
	port := proxyURL.Port()
	if port == "" {
		port = defaultProxyPort(proxyURL.Scheme)
	}
	conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(proxyURL.Hostname(), port))
	if err != nil {
		return nil, err
	}
	connected := false
	defer func() {
		if !connected {
			_ = conn.Close()
		}
	}()
	rawConn := conn
	stopCancel := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	defer stopCancel()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}
	if proxyURL.Scheme == "https" {
		proxyTLSConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		if tlsConfig != nil {
			proxyTLSConfig = tlsConfig.Clone()
		}
		proxyTLSConfig.ServerName = proxyURL.Hostname()
		// CONNECT uses HTTP/1.1 even if the origin transport enables HTTP/2.
		proxyTLSConfig.NextProtos = []string{"http/1.1"}
		tlsConn := tls.Client(conn, proxyTLSConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		conn = tlsConn
	}
	connect := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: address}, Host: address, Header: make(http.Header)}
	if user := proxyURL.User; user != nil {
		password, _ := user.Password()
		connect.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user.Username()+":"+password)))
	}
	if err := connect.Write(conn); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, connect)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy CONNECT failed: %s", response.Status)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	connected = true
	return &proxyTunnelConn{Conn: conn, reader: reader}, nil
}

type proxyTunnelConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *proxyTunnelConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}
