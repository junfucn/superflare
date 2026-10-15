package netutil

import "golang.org/x/sys/windows/registry"

func readSystemProxyConfig() systemProxyConfig {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return systemProxyConfig{}
	}
	defer key.Close()

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return systemProxyConfig{}
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return systemProxyConfig{}
	}
	bypass, _, _ := key.GetStringValue("ProxyOverride")
	return systemProxyConfig{server: server, bypass: bypass}
}
