package app

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func (a *App) login(c *gin.Context) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(c, &input) {
		return
	}
	u, ok := a.passwordUser(c, input.Username, input.Password, "auth.login")
	if !ok {
		return
	}
	c.Set("user", u)
	if !a.issueSession(c, u, "auth.login") {
		return
	}
	a.DB.Exec("DELETE FROM login_attempts WHERE key = ?", TokenHash(c.ClientIP()))
	a.me(c)
}

// 密码登录和首次身份绑定共享限流，不能通过另一入口绕过密码尝试上限。
func (a *App) passwordUser(c *gin.Context, username, password, action string) (User, bool) {
	if c.GetHeader("Origin") != a.Origin {
		fail(c, 403, "请从平台页面登录")
		return User{}, false
	}
	key := TokenHash(c.ClientIP())
	var attempts int
	err := a.DB.Raw(`INSERT INTO login_attempts(key,attempts,expires_at) VALUES(?,1,now()+interval '15 minutes') ON CONFLICT(key) DO UPDATE SET attempts=CASE WHEN login_attempts.expires_at<now() THEN 1 ELSE login_attempts.attempts+1 END,expires_at=CASE WHEN login_attempts.expires_at<now() THEN now()+interval '15 minutes' ELSE login_attempts.expires_at END RETURNING attempts`, key).Scan(&attempts).Error
	if err != nil {
		fail(c, 503, "登录服务暂不可用")
		return User{}, false
	}
	if attempts > 20 {
		fail(c, 429, "登录尝试过多，请 15 分钟后重试")
		return User{}, false
	}
	var u User
	err = a.DB.Preload("Roles").Where("username = ?", username).First(&u).Error
	hash := u.PasswordHash
	if err != nil {
		hash = "$2a$12$W7MZs.YyAEhcOrVn5JBGqe.1PLwc73tMVbKnLS3QYpbJF6Skprp1a"
	}
	valid := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	if !valid || err != nil || !u.Enabled {
		if a.record(c, a.DB, action, "", "failed", "账号或密码错误") != nil {
			fail(c, 503, "审计服务不可用")
			return User{}, false
		}
		fail(c, 401, "账号或密码错误")
		return User{}, false
	}
	return u, true
}

func newSession(userID uint64) (Session, string) {
	token := RandomToken()
	return Session{ID: TokenHash(token), UserID: userID, CSRF: RandomToken(), CreatedAt: time.Now(), LastSeen: time.Now()}, token
}
func (a *App) issueSession(c *gin.Context, u User, action string) bool {
	s, token := newSession(u.ID)
	if !a.mutation(c, action, "", func(tx *gorm.DB) error { return tx.Create(&s).Error }) {
		return false
	}
	a.setSession(c, u, s, token)
	return true
}
func (a *App) setSession(c *gin.Context, u User, s Session, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("opsdeck_session", token, 60*60*24*7, "/", "", a.Secure, true)
	c.Set("session", s)
	c.Set("user", u)
}
func (a *App) me(c *gin.Context) {
	u := current(c)
	s := c.MustGet("session").(Session)
	c.JSON(200, gin.H{"user": u, "csrf_token": s.CSRF})
}
func (a *App) logout(c *gin.Context) {
	s := c.MustGet("session").(Session)
	if a.mutation(c, "auth.logout", "", func(tx *gorm.DB) error { return tx.Delete(&s).Error }) {
		c.SetCookie("opsdeck_session", "", -1, "/", "", a.Secure, true)
		c.Status(204)
	}
}
func (a *App) password(c *gin.Context) {
	u := current(c)
	var in struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if !readJSON(c, &in) {
		return
	}
	if !ValidPassword(in.Password) {
		bad(c, "密码需要 12 至 72 字节，且首尾不能有空白")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Current)) != nil {
		_ = a.record(c, a.DB, "auth.password", "", "failed", "原密码不正确")
		bad(c, "原密码不正确")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), 12)
	if err != nil {
		fail(c, 500, "无法设置密码")
		return
	}
	s := c.MustGet("session").(Session)
	if a.mutation(c, "auth.password", fmt.Sprint(u.ID), func(tx *gorm.DB) error {
		if e := tx.Model(&u).Updates(map[string]any{"password_hash": string(hash), "must_change_password": false}).Error; e != nil {
			return e
		}
		return tx.Where("user_id = ? AND id <> ?", u.ID, s.ID).Delete(&Session{}).Error
	}) {
		c.Status(204)
	}
}
func (a *App) preferences(c *gin.Context) {
	var prefs map[string]any
	if !readJSON(c, &prefs) {
		return
	}
	u := current(c)
	u.Preferences = prefs
	if err := a.DB.Model(&u).Select("Preferences").Updates(&u).Error; err != nil {
		fail(c, 500, "保存偏好失败")
		return
	}
	c.Status(204)
}
