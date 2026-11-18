package testutil

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// ForwardProxyTunnel connects a test proxy to a local origin after the caller
// has checked that the CONNECT request contains the expected pinned address.
func ForwardProxyTunnel(t *testing.T, w http.ResponseWriter, r *http.Request, origin string) {
	t.Helper()
	if r.Method != http.MethodConnect {
		t.Errorf("expected CONNECT, got %s", r.Method)
		http.Error(w, "expected CONNECT", http.StatusBadRequest)
		return
	}
	upstream, err := net.DialTimeout("tcp", origin, time.Second)
	if err != nil {
		t.Error(err)
		http.Error(w, "dial failed", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	conn, buffered, err := w.(http.Hijacker).Hijack()
	if err != nil {
		t.Error(err)
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_ = upstream.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	if err := buffered.Flush(); err != nil {
		t.Error(err)
		return
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(upstream, buffered)
		close(done)
	}()
	_, _ = io.Copy(conn, upstream)
	_ = conn.Close()
	<-done
}
