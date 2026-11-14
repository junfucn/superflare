package templates

import (
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestRenderKeepsPartialTemplatesOutOfErrorResponses(t *testing.T) {
	e := echo.New()
	e.Renderer = &Renderer{templates: template.Must(template.New("page").Funcs(template.FuncMap{
		"fail": func() (string, error) { return "", errors.New("render failed") },
	}).Parse(`{{if .}}partial-output{{fail}}{{else}}<p>complete</p>{{end}}`))}
	e.GET("/", func(c *echo.Context) error {
		return Render(c, http.StatusCreated, "page", c.QueryParam("fail") != "")
	})
	for _, target := range []string{"/?fail=1", "/", "/?fail=1", "/"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if target == "/" {
			require.Equal(t, http.StatusCreated, rec.Code)
			require.Equal(t, "<p>complete</p>", rec.Body.String())
			require.Contains(t, rec.Header().Get("Content-Type"), "text/html")
		} else {
			require.Equal(t, http.StatusInternalServerError, rec.Code)
			require.NotContains(t, rec.Body.String(), "partial-output")
		}
	}
}

func TestRenderConcurrentResponsesStayIndependent(t *testing.T) {
	e := echo.New()
	e.Renderer = &Renderer{templates: template.Must(template.New("page").Parse(`<p>{{.}}</p>`))}
	e.GET("/:id", func(c *echo.Context) error {
		return Render(c, http.StatusOK, "page", strings.Repeat(c.Param("id")+"&", 2048))
	})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			id := strconv.Itoa(i)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+id, nil))
			want := "<p>" + strings.Repeat(id+"&amp;", 2048) + "</p>"
			if rec.Code != http.StatusOK || rec.Body.String() != want {
				t.Errorf("response %s contains incorrect status or another request's data", id)
			}
		})
	}
	wg.Wait()
}
