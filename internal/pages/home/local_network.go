package home

import (
	"net/http"

	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/localnetwork"
)

func hasLocalNetworkLinks(groups ...[]model.Bookmark) bool {
	for _, items := range groups {
		for _, item := range items {
			if shouldUseLocalRedirect(item.URL, item.LocalURL, false, nil) {
				return true
			}
		}
	}
	return false
}

func bindLocalNetwork(m map[string]any, options model.Application, request *http.Request, enabled bool) {
	if !enabled {
		return
	}
	m["HasLocalNetworkLinks"] = true
	m["LocalNetworkScript"] = localnetwork.Script(localnetwork.Config(options, request))
}
