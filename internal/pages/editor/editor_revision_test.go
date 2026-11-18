package editor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestUpdateDataReturnsConflictForStaleOrMissingRevision(t *testing.T) {
	t.Chdir(t.TempDir())
	writeEditorRenderFiles(t, ".")
	initial, err := data.GetEditorSnapshot()
	require.NoError(t, err)
	save := func(revision string, wantsJSON bool) *httptest.ResponseRecorder {
		t.Helper()
		form := url.Values{
			"categories": {"1,Group"},
			"bookmarks":  {"1,Saved,https://saved.invalid/,,Group,,,,false,false"},
			"revision":   {revision},
		}
		req := httptest.NewRequest(http.MethodPost, "/editor", strings.NewReader(form.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		if wantsJSON {
			req.Header.Set(echo.HeaderAccept, echo.MIMEApplicationJSON)
		}
		rec := httptest.NewRecorder()
		require.NoError(t, updateData(echo.New().NewContext(req, rec)))
		return rec
	}
	success := save(initial.Revision, true)
	require.Equal(t, http.StatusOK, success.Code, success.Body.String())
	var response map[string]string
	require.NoError(t, json.Unmarshal(success.Body.Bytes(), &response))
	require.Equal(t, editorNoticeSaveSuccess, response["notice"])
	current, err := data.GetEditorSnapshot()
	require.NoError(t, err)
	require.Equal(t, current.Revision, response["revision"])
	require.NotEmpty(t, response["revision"])

	for _, revision := range []string{initial.Revision, ""} {
		for _, wantsJSON := range []bool{true, false} {
			conflict := save(revision, wantsJSON)
			require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
			if wantsJSON {
				require.Contains(t, conflict.Header().Get(echo.HeaderContentType), echo.MIMEApplicationJSON)
				require.Contains(t, conflict.Body.String(), `"error"`)
				require.NotContains(t, conflict.Body.String(), editorNoticeSaveSuccess)
			} else {
				require.Contains(t, conflict.Body.String(), "status-panel")
			}
			afterConflict, err := data.GetEditorSnapshot()
			require.NoError(t, err)
			require.Equal(t, current, afterConflict)
		}
	}
	repeated := save(response["revision"], true)
	require.Equal(t, http.StatusOK, repeated.Code, repeated.Body.String())
}
