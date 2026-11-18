package appearance

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/labstack/echo/v5"
)

func TestAppearanceGreetingsSurviveHiddenFormField(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("config.yml", []byte("Title: SuperFlare\nLocale: en\nTheme: blackboard\nShowTitle: true\nGreetings: Welcome back\n"), 0644); err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	e.Renderer = appearanceSuccessRenderer{}
	for _, step := range []struct {
		name, showTitle, title, greeting, wantGreeting string
		submitGreeting                                 bool
	}{
		{"hide title", "0", "SuperFlare", "Welcome back", "Welcome back", true},
		{"edit while hidden", "0", "Updated title", "", "Welcome back", false},
		{"show title again", "1", "Updated title", "", "Welcome back", false},
		{"replace greeting", "1", "Updated title", "Hello again", "Hello again", true},
		{"explicitly clear greeting", "1", "Updated title", "", "", true},
	} {
		t.Run(step.name, func(t *testing.T) {
			form := url.Values{
				"icon-mode": {"FILLING"}, "locale": {"en"},
				"title": {step.title}, "show-title": {step.showTitle},
			}
			if step.submitGreeting {
				form.Set("greetings", step.greeting)
			}
			req := httptest.NewRequest(http.MethodPost, "/settings/appearance", strings.NewReader(form.Encode()))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
			rec := httptest.NewRecorder()
			if err := updateAppearanceOptions(e.NewContext(req, rec)); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
			}
			options, err := data.GetAllSettingsOptions()
			if err != nil {
				t.Fatal(err)
			}
			if options.Greetings != step.wantGreeting || options.ShowTitle != (step.showTitle == "1") || options.Title != step.title {
				t.Fatalf("saved greetings=%q, showTitle=%t, title=%q", options.Greetings, options.ShowTitle, options.Title)
			}
		})
	}
}
