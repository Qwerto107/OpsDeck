package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"opsdeck/migrations"
	"testing"
	"time"
)

func bindingFixture(t *testing.T) (*App, User, OAuthProvider) {
	t.Helper()
	a, admin := integrationApp(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code_verifier") == "" {
			t.Error("第三方登录未保留 PKCE 验证")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"test-access-token","token_type":"Bearer"}`))
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access-token" {
			t.Error("用户信息请求未使用授权令牌")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"external-user-12345"}`))
	})
	idp := httptest.NewTLSServer(mux)
	t.Cleanup(idp.Close)
	transport := http.DefaultTransport
	http.DefaultTransport = idp.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = transport })
	secret, err := Encrypt(a.Key, "test-client-secret")
	if err != nil {
		t.Fatal(err)
	}
	provider := OAuthProvider{Name: "测试身份平台", ClientID: "test-client", ClientSecret: secret, AuthURL: idp.URL + "/authorize", TokenURL: idp.URL + "/token", UserURL: idp.URL + "/userinfo", SubjectField: "sub", Enabled: true}
	if err := a.DB.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	provider.RedirectURL = fmt.Sprintf("%s/api/oauth/%d/callback", a.Origin, provider.ID)
	if err := a.DB.Save(&provider).Error; err != nil {
		t.Fatal(err)
	}
	return a, admin, provider
}

func oauthCallbackTest(t *testing.T, a *App, provider OAuthProvider) *httptest.ResponseRecorder {
	t.Helper()
	start := sendRequest(a, "GET", fmt.Sprintf("/api/oauth/%d/start", provider.ID), nil, nil, "")
	requireCode(t, start, 302)
	location, err := url.Parse(start.Header().Get("Location"))
	if err != nil || location.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("授权跳转或 PKCE 参数错误")
	}
	return sendRequest(a, "GET", fmt.Sprintf("/api/oauth/%d/callback?code=test-code&state=%s", provider.ID, location.Query().Get("state")), nil, start.Result().Cookies()[0], "")
}

func bindingRequest(a *App, method string, cookie *http.Cookie, csrf string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "/api/oauth/bind", bytes.NewReader(data))
	r.Header.Set("Origin", a.Origin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-OAuth-Bind-CSRF", csrf)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.Router().ServeHTTP(w, r)
	return w
}

func TestPostgresOAuthFirstLoginBindsExistingAccount(t *testing.T) {
	a, admin, provider := bindingFixture(t)
	callback := oauthCallbackTest(t, a, provider)
	requireCode(t, callback, 302)
	if callback.Header().Get("Location") != "/oauth/bind" {
		t.Fatal("未绑定身份没有进入向导")
	}
	var cookie *http.Cookie
	for _, item := range callback.Result().Cookies() {
		if item.Name == "opsdeck_session" {
			t.Fatal("未验证本地账号就建立了登录会话")
		}
		if item.Name == oauthBindingCookie {
			cookie = item
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.MaxAge != 300 {
		t.Fatal("缺少短期 HttpOnly 绑定凭证")
	}
	info := bindingRequest(a, "GET", cookie, "", nil)
	requireCode(t, info, 200)
	var pending struct {
		CSRF string `json:"csrf_token"`
	}
	if json.Unmarshal(info.Body.Bytes(), &pending) != nil || pending.CSRF == "" {
		t.Fatal("缺少绑定 CSRF 校验")
	}
	input := map[string]any{"username": admin.Username, "password": "test-password-123", "confirm": true}
	requireCode(t, bindingRequest(a, "POST", cookie, "wrong", input), 403)
	input["password"] = "wrong"
	requireCode(t, bindingRequest(a, "POST", cookie, pending.CSRF, input), 401)
	var sessions, identities, users int64
	a.DB.Model(&Session{}).Count(&sessions)
	a.DB.Model(&Identity{}).Count(&identities)
	if sessions != 0 || identities != 0 {
		t.Fatal("错误凭证创建了身份或会话")
	}
	input["password"] = "test-password-123"
	bound := bindingRequest(a, "POST", cookie, pending.CSRF, input)
	requireCode(t, bound, 200)
	a.DB.Model(&User{}).Count(&users)
	if users != 1 {
		t.Fatal("绑定流程创建了新用户")
	}
	var identity Identity
	if a.DB.First(&identity).Error != nil || identity.UserID != admin.ID || identity.Subject != "external-user-12345" {
		t.Fatal("第三方身份没有绑定到验证过的已有账号")
	}
	var sessionCookie *http.Cookie
	for _, item := range bound.Result().Cookies() {
		if item.Name == "opsdeck_session" {
			sessionCookie = item
		}
	}
	requireCode(t, sendRequest(a, "GET", "/api/me", nil, sessionCookie, ""), 200)
	requireCode(t, bindingRequest(a, "POST", cookie, pending.CSRF, input), 410)
	var tickets, audits int64
	a.DB.Model(&OAuthBinding{}).Count(&tickets)
	a.DB.Model(&Audit{}).Where("action IN ? AND result = 'success'", []string{"oauth.link", "oauth.login"}).Count(&audits)
	if tickets != 0 || audits != 2 {
		t.Fatal("绑定凭证没有消费或缺少审计")
	}
	next := oauthCallbackTest(t, a, provider)
	requireCode(t, next, 302)
	if next.Header().Get("Location") != "/servers" {
		t.Fatal("已绑定身份没有直接登录")
	}
}

func TestPostgresOAuthBindingRejectsExpiredAndConflictingIdentity(t *testing.T) {
	a, admin, provider := bindingFixture(t)
	newPending := func() (*http.Cookie, OAuthBinding) {
		raw := RandomToken()
		pending := OAuthBinding{ID: TokenHash(raw), ProviderID: provider.ID, Subject: "same-subject", CSRF: RandomToken(), ExpiresAt: time.Now().Add(5 * time.Minute)}
		if err := a.DB.Create(&pending).Error; err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: oauthBindingCookie, Value: raw}, pending
	}
	cookie, pending := newPending()
	input := map[string]any{"username": admin.Username, "password": "test-password-123", "confirm": true}
	a.DB.Model(&pending).Update("expires_at", time.Now().Add(-time.Minute))
	requireCode(t, bindingRequest(a, "POST", cookie, pending.CSRF, input), 410)
	cookie, pending = newPending()
	other := User{Username: "another-account", Enabled: true, PasswordHash: admin.PasswordHash, Preferences: map[string]any{}}
	if err := a.DB.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	identity := Identity{ProviderID: provider.ID, Subject: pending.Subject, UserID: other.ID}
	if err := a.DB.Create(&identity).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, bindingRequest(a, "POST", cookie, pending.CSRF, input), 409)
	a.DB.First(&identity, identity.ID)
	if identity.UserID != other.ID {
		t.Fatal("已有身份关联被覆盖")
	}
	a.DB.Model(&admin).Update("must_change_password", true)
	requireCode(t, bindingRequest(a, "POST", cookie, pending.CSRF, input), 409)
	a.DB.Model(&admin).Updates(map[string]any{"must_change_password": false, "enabled": false})
	requireCode(t, bindingRequest(a, "POST", cookie, pending.CSRF, input), 401)
	a.DB.Model(&provider).Update("enabled", false)
	requireCode(t, bindingRequest(a, "GET", cookie, "", nil), 410)
	var sessions int64
	a.DB.Model(&Session{}).Count(&sessions)
	if sessions != 0 {
		t.Fatal("无效绑定创建了会话")
	}
}

func TestPostgresSessionDefaultsAndManualSettings(t *testing.T) {
	a, admin := integrationApp(t)
	settings, err := a.settings()
	if err != nil || settings.IdleMinutes != 480 || settings.MaxHours != 72 {
		t.Fatal("会话默认值不是空闲 8 小时、最长 72 小时")
	}
	now := time.Now()
	for _, session := range []Session{
		{CreatedAt: now.Add(-9 * time.Hour), LastSeen: now.Add(-8 * time.Hour)},
		{CreatedAt: now.Add(-72 * time.Hour), LastSeen: now},
	} {
		if SessionValid(session, settings, now) {
			t.Fatal("会话没有在时限到达时失效")
		}
	}
	cookie, csrf := loginTest(t, a, admin.Username)
	settings.IdleMinutes, settings.MaxHours = 120, 24
	requireCode(t, sendRequest(a, "PUT", "/api/settings", settings, cookie, csrf), 200)
	saved, err := a.settings()
	if err != nil || saved.IdleMinutes != 120 || saved.MaxHours != 24 {
		t.Fatal("手动会话设置未保存")
	}
	sql, err := migrations.Files.ReadFile("000005_session_defaults.up.sql")
	if err != nil || a.DB.Exec(string(sql)).Error != nil {
		t.Fatal("无法验证会话默认值迁移")
	}
	saved, err = a.settings()
	if err != nil || saved.IdleMinutes != 120 || saved.MaxHours != 24 {
		t.Fatal("默认值迁移覆盖了已自定义的会话设置")
	}
}
