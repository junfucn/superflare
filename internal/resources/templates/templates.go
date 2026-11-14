package templates

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/labstack/echo/v5"
	Minify "github.com/tdewolff/minify/v2"
	MinifyCSS "github.com/tdewolff/minify/v2/css"
	MinifyHTML "github.com/tdewolff/minify/v2/html"
	MinifySVG "github.com/tdewolff/minify/v2/svg"

	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/i18n"
	"github.com/junfuchang/superflare/internal/pool"
	"github.com/junfuchang/superflare/internal/resources/assets"
	"github.com/junfuchang/superflare/internal/resources/mdi"
)

//go:embed html
var TPL embed.FS

type templateRuntimeSnapshot struct {
	DebugMode bool
}

type templateRuntimeHolder struct {
	mu  sync.RWMutex
	set bool
	cfg templateRuntimeSnapshot
}

func (h *templateRuntimeHolder) Load() templateRuntimeSnapshot {
	if h == nil {
		return templateRuntimeSnapshot{}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.set {
		return templateRuntimeSnapshot{}
	}
	return h.cfg
}

func (h *templateRuntimeHolder) Store(cfg templateRuntimeSnapshot) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.set = true
	h.cfg = cfg
	h.mu.Unlock()
}

var templateRuntimeFlags = &templateRuntimeHolder{}

func templateRuntimeSnapshotFromFlags(flags model.Flags) templateRuntimeSnapshot {
	return templateRuntimeSnapshot{DebugMode: flags.DebugMode}
}

func currentTemplateRuntime() templateRuntimeSnapshot {
	templateRuntimeFlags.mu.RLock()
	hasValue := templateRuntimeFlags.set
	cfg := templateRuntimeFlags.cfg
	templateRuntimeFlags.mu.RUnlock()
	if hasValue {
		return cfg
	}
	cfg = templateRuntimeSnapshotFromFlags(define.CurrentAppRuntimeFlags())
	templateRuntimeFlags.Store(cfg)
	return cfg
}

func SetRuntimeFlags(flags model.Flags) {
	templateRuntimeFlags.Store(templateRuntimeSnapshotFromFlags(flags))
}

// Renderer implements echo.Renderer for HTML templates.
type Renderer struct {
	templates *template.Template
}

func (r *Renderer) execute(w io.Writer, templateName string, data any) error {
	tmplName := templateName
	for _, cand := range []string{templateName, "html/" + templateName, "embed/templates/" + templateName} {
		if r.templates.Lookup(cand) != nil {
			tmplName = cand
			break
		}
	}
	return r.templates.ExecuteTemplate(w, tmplName, data)
}

func (r *Renderer) Render(c *echo.Context, w io.Writer, templateName string, data any) error {
	buf := pool.GetBuffer()
	defer pool.PutBuffer(buf)
	if err := r.execute(buf, templateName, data); err != nil {
		return err
	}
	_, err := buf.WriteTo(w)
	return err
}

// Render buffers the response once and reuses its storage across requests.
// Like echo.Context.Render, it does not commit a response until rendering
// succeeds, so the error handler can still send the appropriate status page.
func Render(c *echo.Context, code int, name string, data any) error {
	renderer := c.Echo().Renderer
	if renderer == nil {
		return echo.ErrRendererNotRegistered
	}
	buf := pool.GetBuffer()
	defer pool.PutBuffer(buf)
	var err error
	if r, ok := renderer.(*Renderer); ok {
		err = r.execute(buf, name, data)
	} else {
		err = renderer.Render(c, buf, name, data)
	}
	if err != nil {
		return err
	}
	return c.HTMLBlob(code, buf.Bytes())
}

var templateFuncMap = template.FuncMap{
	"T":                   i18n.T,
	"IconURL":             mdi.GetIconURLByName,
	"SiteIconURL":         func(name string) string { return assets.SiteIconURL(mdi.GetIconURLByName, name) },
	"AppleTouchIconURL":   assets.AppleTouchIconURL,
	"AndroidChrome192URL": assets.AndroidChrome192URL,
	"AndroidChrome512URL": assets.AndroidChrome512URL,
}

func RegisterRouting(e *echo.Echo) error {
	var t *template.Template
	var err error
	if currentTemplateRuntime().DebugMode {
		if err := ensureGeneratedTemplatesAreFresh(); err != nil {
			return err
		}
		t, err = template.New("").Funcs(templateFuncMap).ParseGlob("embed/templates/*.html")
	} else {
		t, err = template.New("").Funcs(templateFuncMap).ParseFS(TPL, "html/*.html")
	}
	if err != nil {
		return err
	}
	e.Renderer = &Renderer{templates: t}
	return nil
}

func ensureGeneratedTemplatesAreFresh() error {
	for _, name := range []string{
		"editor.html",
		"home.html",
		"settings.html",
		"settings-appearance.html",
		"settings-header.html",
		"settings-others.html",
		"settings-ports.html",
		"settings-search.html",
		"settings-scripts.html",
		"settings-seo.html",
		"settings-sidebar.html",
		"settings-theme.html",
	} {
		srcPath := filepath.Join("embed", "templates", name)
		genPath := filepath.Join("internal", "resources", "templates", "html", name)

		srcRaw, srcErr := os.ReadFile(filepath.Clean(srcPath))
		genRaw, genErr := os.ReadFile(filepath.Clean(genPath))
		if srcErr != nil || genErr != nil {
			return fmt.Errorf("template sync check failed for %s: source err=%v generated err=%v", name, srcErr, genErr)
		}
		minifiedSource, err := MinifyTemplateBytes(srcRaw)
		if err != nil {
			return fmt.Errorf("template minify check failed for %s: %w", name, err)
		}
		if !bytes.Equal(normalizeTemplateBytes(minifiedSource), normalizeTemplateBytes(genRaw)) {
			return fmt.Errorf("template %s is out of sync with generated resources; run `go run .\\build\\build.go`", name)
		}
	}
	return nil
}

func normalizeTemplateBytes(raw []byte) []byte {
	return bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
}

func ReadEmbeddedTemplate(name string) ([]byte, error) {
	return fs.ReadFile(TPL, filepath.ToSlash(filepath.Join("html", name)))
}

func MinifyTemplateBytes(raw []byte) ([]byte, error) {
	m := Minify.New()
	m.Add("text/html", &MinifyHTML.Minifier{
		KeepDocumentTags: true,
		KeepQuotes:       true,
		TemplateDelims:   MinifyHTML.GoTemplateDelims,
	})
	m.AddFunc("text/css", MinifyCSS.Minify)
	m.AddFunc("image/svg+xml", MinifySVG.Minify)
	return m.Bytes("text/html", raw)
}
