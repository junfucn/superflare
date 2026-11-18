package assets

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/background"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestUploadedBackgroundRefreshesAfterReplacement(t *testing.T) {
	t.Chdir(t.TempDir())
	oldRuntime := assetsRuntimeFlags.Load()
	t.Cleanup(func() { assetsRuntimeFlags.Store(oldRuntime) })
	SetRuntimeFlags(model.Flags{})
	putImage := func(fill color.NRGBA) {
		img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				img.SetNRGBA(x, y, fill)
			}
		}
		var buf bytes.Buffer
		require.NoError(t, png.Encode(&buf, img))
		_, err := background.PrepareUploadedBackground("test.png", &buf)
		require.NoError(t, err)
	}
	getImage := func(etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, background.UploadedFullPath, nil)
		req.Header.Set("If-None-Match", etag)
		rec := httptest.NewRecorder()
		require.NoError(t, serveUploadedBackground(echo.New().NewContext(req, rec)))
		return rec
	}
	options := model.Application{BackgroundImage: background.UploadedFullPath, BackgroundImageMode: "upload"}
	putImage(color.NRGBA{R: 200, A: 255})
	firstURL := background.ResolveAssets(options).FullURL
	first := getImage("")
	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, "public, no-cache", first.Header().Get("Cache-Control"))
	etag := first.Header().Get("ETag")
	require.NotEmpty(t, etag)
	require.Equal(t, http.StatusNotModified, getImage(etag).Code)
	putImage(color.NRGBA{B: 220, A: 255})
	require.NotEqual(t, firstURL, background.ResolveAssets(options).FullURL)
	second := getImage(etag)
	require.Equal(t, http.StatusOK, second.Code)
	require.NotEqual(t, etag, second.Header().Get("ETag"))
	require.NotEqual(t, first.Body.Bytes(), second.Body.Bytes())
}
