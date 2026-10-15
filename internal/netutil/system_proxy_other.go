//go:build !windows

package netutil

func readSystemProxyConfig() systemProxyConfig {
	return systemProxyConfig{}
}
