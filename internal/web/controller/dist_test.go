package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
)

func TestDistPageCurrentUserMetadata(t *testing.T) {
	oldDistFS := distFS
	SetDistFS(fstest.MapFS{
		"dist/index.html": {Data: []byte(`<html><head></head></html>`)},
		"dist/login.html": {Data: []byte(`<html><head></head></html>`)},
	})
	t.Cleanup(func() { SetDistFS(oldDistFS) })
	engine := gin.New()
	engine.Use(sessions.Sessions("3x-ui", cookie.NewStore([]byte("account-metadata-test-secret"))))
	engine.GET("/:page", func(c *gin.Context) {
		session.SetAPIAuthUser(c, &model.User{Id: 1, Username: `Admin"><script>alert(1)</script>`, Password: "private-password-hash"})
		serveDistPage(c, c.Param("page"))
	})
	for _, page := range []string{"index.html", "login.html"} {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/"+page, nil))
		body := w.Body.String()
		if w.Code != http.StatusOK || strings.Contains(body, "private-password-hash") {
			t.Fatalf("unexpected response for %s", page)
		}
		if page == "index.html" {
			if !strings.Contains(body, `name="current-user" content="Admin&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;"`) {
				t.Fatalf("username must be escaped in metadata: %s", body)
			}
		} else if strings.Contains(body, `name="current-user"`) {
			t.Fatal("login page must not expose account metadata")
		}
	}
}

func TestWithServerBasePath(t *testing.T) {
	spec := []byte(`{"openapi":"3.0.3","info":{"title":"x"},"servers":[{"url":"/","description":"old"}],"paths":{"/p":{"get":{"summary":"s"}}}}`)

	out, err := withServerBasePath(spec, "/test/")
	if err != nil {
		t.Fatalf("withServerBasePath: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	servers, ok := doc["servers"].([]any)
	if !ok || len(servers) != 1 {
		t.Fatalf("servers = %v, want one entry", doc["servers"])
	}
	srv, _ := servers[0].(map[string]any)
	if srv["url"] != "/test" {
		t.Errorf("server url = %v, want /test (trailing slash trimmed)", srv["url"])
	}

	if doc["openapi"] != "3.0.3" {
		t.Errorf("openapi field not preserved: %v", doc["openapi"])
	}
	if _, ok := doc["paths"].(map[string]any)["/p"]; !ok {
		t.Errorf("paths content not preserved verbatim")
	}
}

func TestWithServerBasePathInvalidJSON(t *testing.T) {
	if _, err := withServerBasePath([]byte("not json"), "/test/"); err == nil {
		t.Errorf("expected error on invalid spec, got nil")
	}
}

func TestPWAHeadInjectionUsesRuntimeBasePath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		wantPath string
	}{
		{name: "root", basePath: "/", wantPath: "/manifest.webmanifest"},
		{name: "secret path", basePath: "panel-secret", wantPath: "/panel-secret/manifest.webmanifest"},
		{name: "trailing slash", basePath: "/panel-secret/", wantPath: "/panel-secret/manifest.webmanifest"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			head := string(pwaHeadInjection(test.basePath, "login.html"))
			if !strings.Contains(head, `href="`+test.wantPath+`"`) {
				t.Fatalf("manifest URL = %q, want %q", head, test.wantPath)
			}
			if !strings.Contains(head, `src="`+strings.Replace(test.wantPath, "manifest.webmanifest", "pwa-register.js", 1)+`"`) {
				t.Fatalf("registration URL = %q", head)
			}
		})
	}
}

func TestPWAHeadInjectionSkipsSubscriptionPage(t *testing.T) {
	if got := pwaHeadInjection("/panel-secret/", "subpage.html"); got != nil {
		t.Fatalf("subpage injection = %q, want nil", got)
	}
}
