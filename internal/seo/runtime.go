package seo

import (
	"strings"
	"sync"

	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
)

// runtimeSnapshot holds the process-wide flags the SEO endpoints need to know
// about. Visibility controls whether private pages such as /applications and
// /bookmarks may be advertised in sitemap.xml and llms.txt.
type runtimeSnapshot struct {
	DebugMode  bool
	Visibility string
}

type runtimeHolder struct {
	mu  sync.RWMutex
	set bool
	cfg runtimeSnapshot
}

func (h *runtimeHolder) Load() runtimeSnapshot {
	if h == nil {
		return runtimeSnapshot{}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.set {
		return runtimeSnapshot{}
	}
	return h.cfg
}

func (h *runtimeHolder) Store(cfg runtimeSnapshot) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.set = true
	h.cfg = cfg
	h.mu.Unlock()
}

var seoRuntimeFlags = &runtimeHolder{}

func runtimeSnapshotFromFlags(flags model.Flags) runtimeSnapshot {
	return runtimeSnapshot{
		DebugMode:  flags.DebugMode,
		Visibility: strings.ToUpper(strings.TrimSpace(flags.Visibility)),
	}
}

func currentRuntime() runtimeSnapshot {
	snapshot := seoRuntimeFlags.Load()
	if snapshot.Visibility != "" || snapshot.DebugMode {
		return snapshot
	}
	snapshot = runtimeSnapshotFromFlags(define.CurrentAppRuntimeFlags())
	seoRuntimeFlags.Store(snapshot)
	return snapshot
}

// SetRuntimeFlags stores the process-wide flags used by the SEO endpoints.
func SetRuntimeFlags(flags model.Flags) {
	seoRuntimeFlags.Store(runtimeSnapshotFromFlags(flags))
}

// VisibilityIsPrivate reports whether the instance hides applications and
// bookmarks behind authentication.
func VisibilityIsPrivate() bool {
	return strings.EqualFold(currentRuntime().Visibility, "PRIVATE")
}
