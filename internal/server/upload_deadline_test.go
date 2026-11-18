package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type uploadDeadlineRecorder struct {
	*httptest.ResponseRecorder
	readDeadline, writeDeadline time.Time
}

func (w *uploadDeadlineRecorder) SetReadDeadline(deadline time.Time) error {
	w.readDeadline = deadline
	return nil
}

func (w *uploadDeadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	w.writeDeadline = deadline
	return nil
}

type checkedUploadBody struct {
	io.Reader
	beforeRead func()
}

func (r checkedUploadBody) Read(p []byte) (int, error) {
	r.beforeRead()
	return r.Reader.Read(p)
}

func (r checkedUploadBody) Close() error { return nil }

func TestUploadDeadlinesApplyBeforeBodyReadOnlyOnAuthorizedUploadRoutes(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	cookie := securityLogin(t, h, "admin", "admin")
	for _, tc := range []struct {
		path       string
		cookie     *http.Cookie
		wantUpload bool
	}{
		{"/settings/theme", cookie, true},
		{"/editor/restore", cookie, true},
		{"/settings/theme", nil, false},
		{"/editor/restore", nil, false},
		{"/editor", cookie, false},
	} {
		t.Run(tc.path+"/"+map[bool]string{true: "authenticated", false: "anonymous"}[tc.cookie != nil], func(t *testing.T) {
			rec := &uploadDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
			started := time.Now()
			check := func() {
				if tc.wantUpload {
					require.WithinDuration(t, started.Add(5*time.Minute), rec.readDeadline, 5*time.Second)
					require.Equal(t, 30*time.Second, rec.writeDeadline.Sub(rec.readDeadline))
				} else {
					require.True(t, rec.readDeadline.IsZero())
					require.True(t, rec.writeDeadline.IsZero())
				}
			}
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			req.Body = checkedUploadBody{Reader: strings.NewReader("invalid=value"), beforeRead: check}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Accept-Encoding", "gzip")
			if tc.cookie != nil {
				req.AddCookie(tc.cookie)
			}
			h.ServeHTTP(rec, req)
			check()
			if tc.cookie == nil {
				require.Equal(t, http.StatusFound, rec.Code)
			} else {
				require.Equal(t, http.StatusBadRequest, rec.Code)
			}
		})
	}
}
