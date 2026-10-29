package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPostgresServiceSettingsApplyImmediately(t *testing.T) {
	a, admin := integrationApp(t)
	cookie, csrf := loginTest(t, a, admin.Username)
	a.Origin = ""
	router := a.Router()
	router.GET("/test/source", func(c *gin.Context) {
		c.JSON(200, gin.H{"ip": sourceIP(c), "url": appURL(c)})
	})
	request := func(method, path, origin string, body any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "http://ops.example"+path, bytes.NewReader(data))
		r.RemoteAddr = "10.0.0.5:23456"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("X-Forwarded-For", "203.0.113.50")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	source := request("GET", "/test/source", "", nil)
	if !bytes.Contains(source.Body.Bytes(), []byte("10.0.0.5")) {
		t.Fatal("未配置代理时信任了伪造转发头")
	}
	requireCode(t, request("POST", "/api/login", "https://evil.example", map[string]string{"username": admin.Username, "password": "test-password-123"}), 403)
	initial := request("POST", "/api/login", "https://ops.example", map[string]string{"username": admin.Username, "password": "test-password-123"})
	requireCode(t, initial, 200)
	if !initial.Result().Cookies()[0].Secure {
		t.Fatal("首次 HTTPS 登录未设置安全 Cookie")
	}
	provider := OAuthProvider{Name: "settings-oauth", ClientID: "client", ClientSecret: "encrypted", AuthURL: "https://id.example/auth", TokenURL: "https://id.example/token", UserURL: "https://id.example/me", RedirectURL: "http://old.example/callback", SubjectField: "sub"}
	if err := a.DB.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	settings, err := a.settings()
	if err != nil {
		t.Fatal(err)
	}
	settings.AppURL = "https://ops.example"
	settings.TrustedProxies = []string{"10.0.0.0/24", "::1"}
	requireCode(t, request("PUT", "/api/settings", "https://ops.example", settings), 200)
	// 沿用同一个 Router，验证保存配置后无需重启进程。
	source = request("GET", "/test/source", "", nil)
	if !bytes.Contains(source.Body.Bytes(), []byte("203.0.113.50")) || !bytes.Contains(source.Body.Bytes(), []byte("https://ops.example")) {
		t.Fatal("保存的可信代理或访问地址未立即生效")
	}
	if err := a.DB.First(&provider, provider.ID).Error; err != nil || provider.RedirectURL != "https://ops.example/api/oauth/1/callback" {
		t.Fatal("访问地址变更后未更新 OAuth2 回调")
	}
	requireCode(t, request("PUT", "/api/preferences", "http://ops.example", map[string]any{}), 403)
	settings.TrustedProxies = []string{"not-an-ip"}
	requireCode(t, request("PUT", "/api/settings", "https://ops.example", settings), 400)
	settings.TrustedProxies = []string{}
	settings.AppURL = "https://ops.example/path"
	requireCode(t, request("PUT", "/api/settings", "https://ops.example", settings), 400)
	settings.AppURL = "https://ops.example"
	requireCode(t, request("PUT", "/api/settings", "https://ops.example", settings), http.StatusOK)
	source = request("GET", "/test/source", "", nil)
	if !bytes.Contains(source.Body.Bytes(), []byte("10.0.0.5")) {
		t.Fatal("清空可信代理后仍信任转发头")
	}
}
