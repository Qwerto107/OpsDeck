package app

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const oauthBindingCookie = "opsdeck_oauth_binding"

var errOAuthBindingExpired = errors.New("oauth binding expired")
var errOAuthIdentityTaken = errors.New("oauth identity already linked")
var errOAuthAccountChanged = errors.New("oauth target account changed")

func (a *App) rejectOAuthBinding(c *gin.Context, status int, message string) {
	result := "failed"
	if status == 403 {
		result = "denied"
	}
	if a.record(c, a.DB, "oauth.link", "", result, message) != nil {
		fail(c, 503, "审计服务不可用")
		return
	}
	fail(c, status, message)
}

func (a *App) bindingCookie(c *gin.Context, value string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oauthBindingCookie, value, maxAge, "/api/oauth/bind", "", a.Secure, true)
}

func (a *App) beginOAuthBinding(c *gin.Context, provider OAuthProvider, subject string) {
	token := RandomToken()
	pending := OAuthBinding{ID: TokenHash(token), ProviderID: provider.ID, Subject: subject, CSRF: RandomToken(), ExpiresAt: time.Now().Add(5 * time.Minute)}
	if a.DB.Create(&pending).Error != nil {
		fail(c, 503, "无法创建绑定请求，请重新发起第三方登录")
		return
	}
	// 待绑定身份只凭短期 HttpOnly Cookie 识别，不把授权令牌或绑定凭证放入 URL。
	a.bindingCookie(c, token, 300)
	c.Redirect(302, "/oauth/bind")
}

func (a *App) oauthBinding(c *gin.Context) (OAuthBinding, OAuthProvider, bool) {
	var pending OAuthBinding
	var provider OAuthProvider
	token, err := c.Cookie(oauthBindingCookie)
	if err != nil || len(token) != 43 {
		fail(c, 410, "绑定请求已失效，请重新发起第三方登录")
		return pending, provider, false
	}
	err = a.DB.Where("id = ? AND expires_at > now()", TokenHash(token)).First(&pending).Error
	if err == nil {
		err = a.DB.Where("id = ? AND enabled = true", pending.ProviderID).First(&provider).Error
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			a.bindingCookie(c, "", -1)
			fail(c, 410, "绑定请求已失效或登录方式已停用，请重新登录")
		} else {
			fail(c, 503, "无法读取绑定请求，请稍后重试")
		}
		return pending, provider, false
	}
	return pending, provider, true
}

func (a *App) pendingOAuthBinding(c *gin.Context) {
	pending, provider, ok := a.oauthBinding(c)
	if !ok {
		return
	}
	hint := []rune(pending.Subject)
	if len(hint) > 4 {
		hint = hint[len(hint)-4:]
	}
	c.JSON(200, gin.H{"provider_name": provider.Name, "subject_hint": "尾号 " + string(hint), "csrf_token": pending.CSRF, "expires_at": pending.ExpiresAt})
}

func (a *App) bindOAuthIdentity(c *gin.Context) {
	if c.GetHeader("Origin") != a.Origin {
		a.rejectOAuthBinding(c, 403, "请从平台页面完成绑定")
		return
	}
	pending, _, ok := a.oauthBinding(c)
	if !ok {
		return
	}
	if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-OAuth-Bind-CSRF")), []byte(pending.CSRF)) != 1 {
		a.rejectOAuthBinding(c, 403, "绑定校验失败，请重新打开绑定页面")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Confirm  bool   `json:"confirm"`
	}
	if !readJSON(c, &input) {
		return
	}
	if !input.Confirm {
		a.rejectOAuthBinding(c, 400, "请确认将此第三方身份绑定到所填写的已有账号")
		return
	}
	u, ok := a.passwordUser(c, input.Username, input.Password, "oauth.link")
	if !ok {
		return
	}
	c.Set("user", u)
	if u.MustChangePassword {
		a.rejectOAuthBinding(c, 409, "请先使用账号密码登录并修改临时密码，再重新发起第三方登录")
		return
	}
	session, token := newSession(u.ID)
	err := a.DB.Transaction(func(tx *gorm.DB) error {
		// 锁定并再次核对凭证、用户和提供方；身份绑定、凭证消费及会话创建同时提交。
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND expires_at > now()", pending.ID).First(&pending).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errOAuthBindingExpired
			}
			return err
		}
		var provider OAuthProvider
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND enabled = true", pending.ProviderID).First(&provider).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errOAuthBindingExpired
			}
			return err
		}
		var target User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, u.ID).Error; err != nil {
			return err
		}
		if !target.Enabled || target.MustChangePassword || target.PasswordHash != u.PasswordHash {
			return errOAuthAccountChanged
		}
		identity := Identity{ProviderID: pending.ProviderID, UserID: u.ID, Subject: pending.Subject}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&identity)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errOAuthIdentityTaken
		}
		if err := tx.Delete(&pending).Error; err != nil {
			return err
		}
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		if err := a.record(c, tx, "oauth.link", fmt.Sprint(pending.ProviderID), "success", "首次第三方登录绑定已有账号"); err != nil {
			return err
		}
		return a.record(c, tx, "oauth.login", "", "success", "")
	})
	if err != nil {
		if a.record(c, a.DB, "oauth.link", fmt.Sprint(pending.ProviderID), "failed", "绑定未完成") != nil {
			fail(c, 503, "审计服务不可用")
			return
		}
		switch {
		case errors.Is(err, errOAuthBindingExpired):
			a.bindingCookie(c, "", -1)
			fail(c, 410, "绑定请求已失效，请重新发起第三方登录")
		case errors.Is(err, errOAuthIdentityTaken):
			a.bindingCookie(c, "", -1)
			fail(c, 409, "该第三方身份已有关联，请重新发起第三方登录")
		case errors.Is(err, errOAuthAccountChanged):
			fail(c, 409, "账号状态已变化，请重新使用账号密码登录")
		default:
			fail(c, 503, "暂时无法完成绑定，请稍后重试")
		}
		return
	}
	a.DB.Exec("DELETE FROM login_attempts WHERE key = ?", TokenHash(c.ClientIP()))
	a.bindingCookie(c, "", -1)
	a.setSession(c, u, session, token)
	a.me(c)
}
