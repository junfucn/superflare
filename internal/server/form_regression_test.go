package server

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
	"github.com/stretchr/testify/require"
)

type formCountingReader struct {
	io.Reader
	read int
}

func TestSearchQueryLimitCountsUnicodeCharacters(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(true, "DEFAULT", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	for _, mode := range []string{"bookmarks", "engine"} {
		require.NoError(t, data.UpdateSearch(true, false, mode, "bing", "same-tab", ""))
		for _, char := range []string{"a", "\u4e2d", "\U0001F600"} {
			for _, count := range []int{50, 51} {
				query := strings.Repeat(char, count)
				form := url.Values{"search": {"  " + query + "  "}}
				rec := securityRequest(h, http.MethodPost, "/", form.Encode(), nil)
				status := http.StatusOK
				if mode == "engine" {
					status = http.StatusFound
				}
				if count > 50 {
					status = http.StatusBadRequest
				}
				require.Equal(t, status, rec.Code, "mode=%s char=%s count=%d", mode, char, count)
				if status == http.StatusFound {
					target, err := url.Parse(rec.Header().Get("Location"))
					require.NoError(t, err)
					require.Equal(t, query, target.Query().Get("q"))
				}
				if status == http.StatusOK {
					require.Contains(t, rec.Body.String(), `pattern="\s*[\s\S]{0,50}\s*"`)
				}
			}
		}
	}
}

func (r *formCountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += n
	return n, err
}

func TestAnonymousSearchRejectsUploadsAndBoundsForms(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "PRIVATE", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	for _, tc := range []struct {
		name, contentType, body string
		length                  int64
		status, maxRead         int
	}{
		{"multipart", "multipart/form-data; boundary=probe", "--probe\r\n", -1, http.StatusUnsupportedMediaType, 0},
		{"known-large", "application/x-www-form-urlencoded", "search=x", 1 << 20, http.StatusRequestEntityTooLarge, 0},
		{"streamed-large", "application/x-www-form-urlencoded", "search=x&extra=" + strings.Repeat("x", 1<<16), -1, http.StatusRequestEntityTooLarge, (8 << 10) + 1},
		{"malformed", "application/x-www-form-urlencoded", "search=%zz", -1, http.StatusBadRequest, 64},
		{"valid", "application/x-www-form-urlencoded", "search=test", -1, http.StatusOK, 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &formCountingReader{Reader: strings.NewReader(tc.body)}
			req := httptest.NewRequest(http.MethodPost, "/", body)
			req.ContentLength = tc.length
			req.Header.Set("Content-Type", tc.contentType)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code)
			require.LessOrEqual(t, body.read, tc.maxRead)
			require.Nil(t, req.MultipartForm)
		})
	}
}

func TestEditorMissingFieldsPreserveDataButExplicitEmptyClears(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	seed := model.Bookmarks{Items: []model.Bookmark{{Name: "Keep", URL: "https://example.invalid"}}}
	require.NoError(t, data.SaveFavoriteBookmarks(seed))
	require.NoError(t, data.SaveNormalBookmarks(seed))
	cookie := securityLogin(t, h, "admin", "admin")
	for _, body := range []string{"", "categories=", "bookmarks="} {
		rec := securityRequest(h, http.MethodPost, "/editor", body, cookie)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		apps, err := data.LoadFavoriteBookmarks()
		require.NoError(t, err)
		require.Equal(t, seed.Items, apps.Items)
		bookmarks, err := data.LoadNormalBookmarks()
		require.NoError(t, err)
		require.Equal(t, seed.Items, bookmarks.Items)
	}
	snapshot, err := data.GetEditorSnapshot()
	require.NoError(t, err)
	rec := securityRequest(h, http.MethodPost, "/editor", "categories=&bookmarks=&revision="+snapshot.Revision, cookie)
	require.Equal(t, http.StatusFound, rec.Code)
	apps, err := data.LoadFavoriteBookmarks()
	require.NoError(t, err)
	require.Empty(t, apps.Items)
	bookmarks, err := data.LoadNormalBookmarks()
	require.NoError(t, err)
	require.Empty(t, bookmarks.Items)
}

type uploadZeroReader struct{}

func (uploadZeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestManagementUploadsBoundEntireMultipartRequest(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	cookie := securityLogin(t, h, "admin", "admin")
	for _, route := range []struct {
		path  string
		limit int64
	}{
		{"/editor/restore", 65 << 20},
		{define.SettingPages.Theme.Path, 33 << 20},
	} {
		for _, knownLength := range []bool{true, false} {
			name := route.path + "/streamed"
			if knownLength {
				name = route.path + "/known-length"
			}
			t.Run(name, func(t *testing.T) {
				tempDir := t.TempDir()
				for _, key := range []string{"TMP", "TEMP", "TMPDIR"} {
					t.Setenv(key, tempDir)
				}
				before, err := os.ReadFile("config.yml")
				require.NoError(t, err)
				beforeBookmarks, err := os.ReadFile("bookmarks.yml")
				require.NoError(t, err)
				var form bytes.Buffer
				writer := multipart.NewWriter(&form)
				require.NoError(t, writer.WriteField("theme", "blackboard"))
				part, err := writer.CreateFormFile("backup", "bookmarks.yml")
				require.NoError(t, err)
				_, err = io.WriteString(part, "categories: []\nlinks: []\n")
				require.NoError(t, err)
				_, err = writer.CreateFormFile("extra", "ignored.bin")
				require.NoError(t, err)
				boundaryOffset := form.Len()
				require.NoError(t, writer.Close())
				extraSize := route.limit + 4096
				body := &formCountingReader{Reader: io.MultiReader(
					bytes.NewReader(form.Bytes()[:boundaryOffset]),
					io.LimitReader(uploadZeroReader{}, extraSize),
					bytes.NewReader(form.Bytes()[boundaryOffset:]),
				)}
				req := httptest.NewRequest(http.MethodPost, route.path, body)
				req.Header.Set("Content-Type", writer.FormDataContentType())
				req.AddCookie(cookie)
				if knownLength {
					req.ContentLength = int64(form.Len()) + extraSize
				} else {
					req.ContentLength = -1
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
				if knownLength {
					require.Zero(t, body.read)
				} else {
					require.Equal(t, route.limit+1, int64(body.read))
				}
				require.Nil(t, req.MultipartForm)
				files, err := os.ReadDir(tempDir)
				require.NoError(t, err)
				require.Empty(t, files, "multipart overflow must not leave temporary uploads")
				after, err := os.ReadFile("config.yml")
				require.NoError(t, err)
				require.Equal(t, before, after)
				afterBookmarks, err := os.ReadFile("bookmarks.yml")
				require.NoError(t, err)
				require.Equal(t, beforeBookmarks, afterBookmarks)
			})
		}
	}
}

func TestRawConfigRestoreUsesBackupEntryLimit(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	cookie := securityLogin(t, h, "admin", "admin")
	for _, tc := range []struct {
		name   string
		size   int
		status int
	}{
		{"at-limit", 8 << 20, http.StatusFound},
		{"over-limit", (8 << 20) + 1, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := os.ReadFile("bookmarks.yml")
			require.NoError(t, err)
			raw := bytes.Repeat([]byte("x"), tc.size)
			copy(raw, "categories: []\nlinks: []\n#")
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("backup", "bookmarks.yml")
			require.NoError(t, err)
			_, err = part.Write(raw)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			req := httptest.NewRequest(http.MethodPost, "/editor/restore", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code)
			after, err := os.ReadFile("bookmarks.yml")
			require.NoError(t, err)
			if tc.status == http.StatusFound {
				require.Equal(t, raw, after)
			} else {
				require.Equal(t, before, after)
			}
			backup := securityRequest(h, http.MethodGet, "/editor/backup", "", cookie)
			require.Equal(t, http.StatusOK, backup.Code)
			require.Equal(t, "application/zip", backup.Header().Get("Content-Type"))
		})
	}
}
