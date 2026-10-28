//go:build with_frontend

package app

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedFrontendWithoutExternalDirectory(t *testing.T) {
	t.Setenv("WEB_DIR", "")
	router := (&App{Origin: "http://localhost:8080"}).Router()
	for _, path := range []string{"/", "/login", "/servers/1"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "id=\"app\"") {
			t.Fatalf("内嵌页面未返回：%s，状态 %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/fonts/inter.woff2", nil))
	if w.Code != 200 || w.Body.Len() == 0 {
		t.Fatal("内嵌字体不可用")
	}
}
