package app

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticAssetsAndSPAFallback(t *testing.T) {
	root := t.TempDir()
	if e := os.MkdirAll(filepath.Join(root, "assets"), 0700); e != nil {
		t.Fatal(e)
	}
	for file, body := range map[string]string{"index.html": "<!doctype html><title>OpsDeck</title>", "assets/app.js": "console.log('loaded')"} {
		if e := os.WriteFile(filepath.Join(root, file), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	t.Setenv("WEB_DIR", root)
	a := &App{Origin: "http://localhost:8080"}
	router := a.Router()
	for _, path := range []string{"/servers", "/login"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "<!doctype html>") {
			t.Fatalf("前端路由未返回入口：%s", path)
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/assets/app.js", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("JS 文件的 MIME 不正确：%d %s", w.Code, w.Header().Get("Content-Type"))
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/assets/missing.js", nil))
	if w.Code != 404 {
		t.Fatal("缺失资源不能返回 HTML 入口")
	}
}
