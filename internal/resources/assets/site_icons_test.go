package assets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func TestSiteIconStreamDeliversFastIconBeforeSlowSourcesFinish(t *testing.T) {
	setupAssetsConfigDir(t)
	const slowCount = 8
	const icon = `<svg xmlns="http://www.w3.org/2000/svg"><title>stream-icon</title></svg>`
	release := make(chan struct{})
	allSlowStarted := make(chan struct{})
	var slowStarted atomic.Int32
	var releaseOnce sync.Once
	releaseSlow := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("source") == "fast" {
			select {
			case <-allSlowStarted:
			case <-r.Context().Done():
				return
			}
		} else {
			if slowStarted.Add(1) == slowCount {
				close(allSlowStarted)
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(icon))
	}))
	defer upstream.Close()
	defer releaseSlow()

	app := echo.New()
	app.Use(middleware.GzipWithConfig(middleware.GzipConfig{MinLength: 1024}))
	RegisterRouting(app)
	server := httptest.NewServer(app)
	defer server.Close()
	defer releaseSlow()

	var sources []string
	for index := 0; index < slowCount; index++ {
		sources = append(sources, fmt.Sprintf("%s/favicon.ico?source=slow-%d", upstream.URL, index))
	}
	fastSource := upstream.URL + "/favicon.ico?source=fast"
	sources = append(sources, fastSource, fastSource)
	configureTestIcons(t, sources...)
	body, err := json.Marshal(map[string]any{"sources": sources})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Post(server.URL+"/assets/site-icons/stream", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request stream while slow icons are pending: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("stream response: status=%d headers=%v", response.StatusCode, response.Header)
	}
	decoder := json.NewDecoder(response.Body)
	var first siteIconStreamResult
	if err := decoder.Decode(&first); err != nil {
		t.Fatalf("read first streamed icon: %v", err)
	}
	if first.Source != fastSource || first.ContentType != "image/svg+xml" || string(first.Data) != icon {
		t.Fatalf("first streamed result = %+v, want fast icon without waiting for slow sources", first)
	}
	releaseSlow()
	for index := 0; index < slowCount; index++ {
		var result siteIconStreamResult
		if err := decoder.Decode(&result); err != nil || string(result.Data) != icon {
			t.Fatalf("slow streamed result %d: err=%v result=%+v", index, err, result)
		}
	}
	var extra siteIconStreamResult
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("stream should end after unique sources: err=%v result=%+v", err, extra)
	}
}

func TestSiteIconStreamRejectsInvalidInput(t *testing.T) {
	tooMany, err := json.Marshal(map[string]any{"sources": make([]string, siteIconStreamMaxSources+1)})
	if err != nil {
		t.Fatal(err)
	}
	app := echo.New()
	RegisterRouting(app)
	for _, body := range []string{
		`{`,
		`{"sources":[]}`,
		`{"sources":["file:///private"]}`,
		`{"sources":["https://user:pass@example.com/favicon.ico"]}`,
		`{"sources":["https://example.com/favicon.ico"]} {}`,
		string(tooMany),
		strings.Repeat(" ", 64*1024+1),
	} {
		req := httptest.NewRequest(http.MethodPost, "/assets/site-icons/stream", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid stream input: status=%d body=%q", rec.Code, rec.Body.String())
		}
	}
}

func TestSiteIconStreamExtendsServerWriteDeadline(t *testing.T) {
	setupAssetsConfigDir(t)
	const icon = `<svg xmlns="http://www.w3.org/2000/svg"><title>late-icon</title></svg>`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(100 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = io.WriteString(w, icon)
	}))
	defer upstream.Close()

	app := echo.New()
	app.Use(middleware.GzipWithConfig(middleware.GzipConfig{MinLength: 1024}))
	RegisterRouting(app)
	server := httptest.NewUnstartedServer(app)
	server.Config.WriteTimeout = 20 * time.Millisecond
	server.Start()
	defer server.Close()
	configureTestIcons(t, upstream.URL+"/favicon.ico")

	body, err := json.Marshal(map[string]any{"sources": []string{upstream.URL + "/favicon.ico"}})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Post(server.URL+"/assets/site-icons/stream", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("stream should outlive the ordinary server write deadline: %v", err)
	}
	defer response.Body.Close()
	var result siteIconStreamResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || string(result.Data) != icon {
		t.Fatalf("late result was truncated: err=%v result=%+v", err, result)
	}
}
