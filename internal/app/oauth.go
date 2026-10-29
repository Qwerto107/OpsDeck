package app

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

func (a *App) publicProviders(c *gin.Context) {
	var rows []OAuthProvider
	if a.DB.Where("enabled = true").Find(&rows).Error != nil {
		fail(c, 503, "无法读取登录方式")
		return
	}
	out := []gin.H{}
	for _, r := range rows {
		out = append(out, gin.H{"id": r.ID, "name": r.Name})
	}
	c.JSON(200, out)
}
func (a *App) providers(c *gin.Context) {
	var rows []OAuthProvider
	if a.DB.Order("id").Find(&rows).Error != nil {
		fail(c, 503, "无法读取身份提供方")
		return
	}
	c.JSON(200, rows)
}
func (a *App) saveProvider(c *gin.Context) {
	var in struct {
		OAuthProvider
		Secret string `json:"client_secret"`
	}
	if !readJSON(c, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" || in.ClientID == "" || in.SubjectField == "" || !ValidEndpoint(in.AuthURL) || !ValidEndpoint(in.TokenURL) || !ValidEndpoint(in.UserURL) {
		bad(c, "请填写 HTTPS 端点及用户唯一标识字段")
		return
	}
	var old OAuthProvider
	if c.Param("id") != "" {
		if a.DB.First(&old, c.Param("id")).Error != nil {
			fail(c, 404, "提供方不存在")
			return
		}
		in.ID = old.ID
	} else {
		in.ID = 0
	}
	in.ClientSecret = old.ClientSecret
	if in.Secret != "" {
		var e error
		in.ClientSecret, e = Encrypt(a.Key, in.Secret)
		if e != nil {
			fail(c, 500, "加密失败")
			return
		}
	}
	if in.ClientSecret == "" {
		bad(c, "请输入 Client Secret")
		return
	}
	if a.mutation(c, "oauth.provider.save", in.Name, func(tx *gorm.DB) error {
		if e := tx.Save(&in.OAuthProvider).Error; e != nil {
			return e
		}
		in.RedirectURL = fmt.Sprintf("%s/api/oauth/%d/callback", a.Origin, in.ID)
		return tx.Model(&in.OAuthProvider).Update("redirect_url", in.RedirectURL).Error
	}) {
		c.JSON(200, in.OAuthProvider)
	}
}
func (a *App) oauthConfig(p OAuthProvider) (*oauth2.Config, error) {
	secret, e := Decrypt(a.Key, p.ClientSecret)
	if e != nil {
		return nil, e
	}
	return &oauth2.Config{ClientID: p.ClientID, ClientSecret: secret, RedirectURL: p.RedirectURL, Scopes: strings.Fields(p.Scopes), Endpoint: oauth2.Endpoint{AuthURL: p.AuthURL, TokenURL: p.TokenURL}}, nil
}
func (a *App) oauthStart(c *gin.Context) {
	var p OAuthProvider
	if a.DB.Where("enabled = true").First(&p, c.Param("id")).Error != nil {
		fail(c, 404, "登录方式不可用")
		return
	}
	conf, e := a.oauthConfig(p)
	if e != nil {
		fail(c, 503, "无法读取登录配置")
		return
	}
	state := RandomToken()
	row := OAuthState{ID: TokenHash(state), ProviderID: p.ID, Verifier: oauth2.GenerateVerifier(), ExpiresAt: time.Now().Add(5 * time.Minute)}
	if c.Query("link") == "true" {
		token, err := c.Cookie("opsdeck_session")
		var s Session
		var u User
		settings, errSettings := a.settings()
		if err != nil || errSettings != nil || a.DB.First(&s, "id = ?", TokenHash(token)).Error != nil || !SessionValid(s, settings, time.Now()) || a.DB.First(&u, s.UserID).Error != nil || !u.Enabled || u.MustChangePassword {
			fail(c, 401, "请先登录并完成密码修改")
			return
		}
		row.UserID = &u.ID
		row.SessionID = s.ID
	}
	if a.DB.Create(&row).Error != nil {
		fail(c, 503, "无法发起授权")
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("opsdeck_oauth", state, 300, "/api/oauth/", "", a.Secure, true)
	c.Redirect(302, conf.AuthCodeURL(state, oauth2.S256ChallengeOption(row.Verifier)))
}
func (a *App) oauthCallback(c *gin.Context) {
	failure := func() {
		_ = a.record(c, a.DB, "oauth.login", "", "failed", "第三方授权失败或身份未关联")
		c.Redirect(302, "/login?error=oauth")
	}
	raw := c.Query("state")
	cookie, e := c.Cookie("opsdeck_oauth")
	if e != nil || raw == "" || subtle.ConstantTimeCompare([]byte(raw), []byte(cookie)) != 1 {
		failure()
		return
	}
	c.SetCookie("opsdeck_oauth", "", -1, "/api/oauth/", "", a.Secure, true)
	var state OAuthState
	// DELETE RETURNING 原子消费 state，授权回调只能使用一次。
	if a.DB.Raw("DELETE FROM oauth_states WHERE id = ? AND expires_at > now() RETURNING *", TokenHash(raw)).Scan(&state).Error != nil || state.ID == "" || fmt.Sprint(state.ProviderID) != c.Param("id") || c.Query("code") == "" {
		failure()
		return
	}
	var p OAuthProvider
	if a.DB.Where("enabled = true").First(&p, state.ProviderID).Error != nil {
		failure()
		return
	}
	conf, e := a.oauthConfig(p)
	if e != nil {
		failure()
		return
	}
	hc := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithTimeout(context.WithValue(c.Request.Context(), oauth2.HTTPClient, hc), 30*time.Second)
	defer cancel()
	token, e := conf.Exchange(ctx, c.Query("code"), oauth2.VerifierOption(state.Verifier))
	if e != nil {
		failure()
		return
	}
	req, e := http.NewRequestWithContext(ctx, "GET", p.UserURL, nil)
	if e != nil {
		failure()
		return
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	response, e := hc.Do(req)
	if e != nil {
		failure()
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		failure()
		return
	}
	var info map[string]any
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	decoder.UseNumber()
	if decoder.Decode(&info) != nil {
		failure()
		return
	}
	var value any = info
	for _, part := range strings.Split(p.SubjectField, ".") {
		obj, ok := value.(map[string]any)
		if !ok {
			failure()
			return
		}
		value = obj[part]
	}
	subject := ""
	switch v := value.(type) {
	case string:
		subject = v
	case json.Number:
		subject = string(v)
	}
	if subject == "" || len(subject) > 512 {
		failure()
		return
	}
	if state.UserID != nil {
		sessionCookie, e := c.Cookie("opsdeck_session")
		var s Session
		var u User
		settings, es := a.settings()
		if e != nil || es != nil || TokenHash(sessionCookie) != state.SessionID || a.DB.First(&s, "id = ?", state.SessionID).Error != nil || !SessionValid(s, settings, time.Now()) || s.UserID != *state.UserID || a.DB.First(&u, *state.UserID).Error != nil || !u.Enabled {
			failure()
			return
		}
		c.Set("user", u)
		if !a.mutation(c, "oauth.link", strconv.FormatUint(p.ID, 10), func(tx *gorm.DB) error {
			return tx.Create(&Identity{ProviderID: p.ID, UserID: u.ID, Subject: subject}).Error
		}) {
			return
		}
		c.Redirect(302, "/profile?linked=true")
		return
	}
	var identity Identity
	var u User
	err := a.DB.Where("provider_id = ? AND subject = ?", p.ID, subject).First(&identity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		a.beginOAuthBinding(c, p, subject)
		return
	}
	if err != nil || a.DB.Preload("Roles").First(&u, identity.UserID).Error != nil || !u.Enabled {
		failure()
		return
	}
	c.Set("user", u)
	if a.issueSession(c, u, "oauth.login") {
		c.Redirect(302, "/servers")
	}
}
