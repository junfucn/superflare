package theme

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/internal/background"
)

type pausedThemeRequestBody struct {
	io.Reader
	started chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (b *pausedThemeRequestBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.resume
	return b.Reader.Read(p)
}

func TestThemeUploadDoesNotHoldBackgroundLockWhileReadingBody(t *testing.T) {
	for _, multipartUpload := range []bool{false, true} {
		t.Run(map[bool]string{false: "urlencoded", true: "multipart upload"}[multipartUpload], func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := data.EnsureAppConfigExists(); err != nil {
				t.Fatal(err)
			}
			var reader io.Reader = strings.NewReader("theme=onedark")
			contentType := echo.MIMEApplicationForm
			wantTheme := "onedark"
			if multipartUpload {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				for key, value := range map[string]string{"action": "custom-theme", "theme": "custom"} {
					if err := writer.WriteField(key, value); err != nil {
						t.Fatal(err)
					}
				}
				part, err := writer.CreateFormFile("background-file", "wallpaper.png")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := part.Write(testPNGBytes(t)); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				reader, contentType, wantTheme = &body, writer.FormDataContentType(), "custom"
			}
			body := &pausedThemeRequestBody{Reader: reader, started: make(chan struct{}), resume: make(chan struct{})}
			req := httptest.NewRequest(http.MethodPost, "/settings/theme", body)
			req.Header.Set(echo.HeaderContentType, contentType)
			rec := httptest.NewRecorder()
			e := echo.New()
			e.Renderer = themeTestRenderer{}
			c := e.NewContext(req, rec)
			finished := make(chan error, 1)
			go func() { finished <- updateThemes(c) }()
			select {
			case <-body.started:
			case <-time.After(2 * time.Second):
				close(body.resume)
				<-finished
				t.Fatal("theme handler did not start reading the request")
			}
			acquired := make(chan struct{})
			go func() {
				_ = background.WithUploadedBackgroundLock(func() error { close(acquired); return nil })
			}()
			select {
			case <-acquired:
			case <-time.After(2 * time.Second):
				t.Error("unfinished theme upload blocked the lock used by backups and restores")
			}
			close(body.resume)
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			<-acquired
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("theme save failed: %d %s", rec.Code, rec.Body.String())
			}
			options, err := data.GetAllSettingsOptions()
			if err != nil || options.Theme != wantTheme {
				t.Fatalf("theme not saved: got %q, err=%v", options.Theme, err)
			}
			if multipartUpload {
				if !background.UsesUploadedBackground(options) {
					t.Fatal("uploaded background was not saved")
				}
				if err := background.CheckUploadedBackground(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
