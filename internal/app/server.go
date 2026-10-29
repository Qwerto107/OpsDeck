package app

import (
	"crypto/cipher"
	"crypto/subtle"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"opsdeck/internal/webui"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type App struct {
	DB     *gorm.DB
	Key    cipher.AEAD
	Origin string
	Secure bool
	Cloud  Cloud
}

func (a *App) settings() (Settings, error) {
	var s Settings
	err := a.DB.First(&s, 1).Error
	return s, err
}
func fail(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": message})
}
func bad(c *gin.Context, message string) { fail(c, 400, message) }
func current(c *gin.Context) User        { return c.MustGet("user").(User) }
func readJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		bad(c, "请求格式不正确")
		return false
	}
	return true
}

func (a *App) Router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				// 不转储请求头或异常内容，避免恢复路径泄露认证凭证。
				slog.Error("http panic recovered", "request_id", c.GetString("request_id"))
				fail(c, http.StatusInternalServerError, "服务暂时不可用")
			}
		}()
		c.Next()
	})
	// 只信任显式配置的反向代理，防止伪造来源 IP 绕过登录限流。
	_ = r.SetTrustedProxies(nil)
	if proxies := os.Getenv("TRUSTED_PROXIES"); proxies != "" {
		if err := r.SetTrustedProxies(strings.Split(proxies, ",")); err != nil {
			panic(err)
		}
	}
	r.Use(func(c *gin.Context) {
		if id := c.Param("id"); id != "" {
			if n, e := strconv.ParseUint(id, 10, 64); e != nil || n == 0 {
				bad(c, "无效记录编号")
				return
			}
		}
		id := RandomToken()
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "same-origin")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Header("Cache-Control", "no-store")
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" && c.Request.Method != "OPTIONS" {
			if origin := c.GetHeader("Origin"); origin != "" && origin != a.Origin {
				fail(c, 403, "请求来源不受信任")
				return
			}
		}
		started := time.Now()
		c.Next()
		if c.Writer.Status() >= 400 && c.Request.Method != "GET" && strings.HasPrefix(c.Request.URL.Path, "/api/") {
			if _, authenticated := c.Get("user"); authenticated && !c.GetBool("audited") {
				if e := a.record(c, a.DB, "request.rejected", c.FullPath(), "failed", "请求未通过校验"); e != nil {
					slog.Error("rejected request audit failed")
				}
			}
		}
		slog.Info("http", "request_id", id, "method", c.Request.Method, "path", c.Request.URL.Path, "status", c.Writer.Status(), "duration", time.Since(started))
	})
	r.GET("/healthz", func(c *gin.Context) {
		db, err := a.DB.DB()
		if err != nil || db.PingContext(c.Request.Context()) != nil {
			fail(c, 503, "数据库不可用")
			return
		}
		c.JSON(200, gin.H{"status": "ok"})
	})
	api := r.Group("/api")
	api.POST("/login", a.login)
	api.GET("/oauth/providers", a.publicProviders)
	api.GET("/oauth/bind", a.pendingOAuthBinding)
	api.POST("/oauth/bind", a.bindOAuthIdentity)
	api.GET("/oauth/:id/start", a.oauthStart)
	api.GET("/oauth/:id/callback", a.oauthCallback)
	auth := api.Group("")
	auth.Use(a.authenticate)
	auth.GET("/me", a.me)
	auth.POST("/logout", a.logout)
	auth.POST("/password", a.password)
	auth.POST("/activity", func(c *gin.Context) {
		s := c.MustGet("session").(Session)
		if err := a.DB.Model(&s).Update("last_seen", time.Now()).Error; err != nil {
			fail(c, 503, "无法更新会话")
			return
		}
		c.Status(204)
	})
	auth.PUT("/preferences", a.preferences)
	auth.GET("/identities", a.identities)
	auth.DELETE("/identities/:id", a.require("users.manage"), a.unlink)
	auth.POST("/resources/sync", a.refreshResources)
	auth.GET("/sync-tasks", a.syncTasks)
	auth.GET("/resources", a.resources)
	auth.GET("/resources/:id", a.resourceDetail)
	auth.POST("/resources/:id/actions", a.action)
	auth.GET("/accounts", a.accounts)
	auth.POST("/accounts", a.require("accounts.manage"), a.saveAccount)
	auth.PUT("/accounts/:id", a.require("accounts.manage"), a.saveAccount)
	auth.DELETE("/accounts/:id", a.require("accounts.manage"), a.deleteAccount)
	auth.POST("/accounts/:id/sync", a.require("accounts.manage"), a.syncAccount)
	auth.GET("/users", a.require("users.manage"), a.users)
	auth.POST("/users", a.require("users.manage"), a.saveUser)
	auth.PUT("/users/:id", a.require("users.manage"), a.saveUser)
	auth.DELETE("/users/:id", a.require("users.manage"), a.deleteUser)
	auth.POST("/users/:id/reset-password", a.require("users.manage"), a.resetPassword)
	auth.GET("/roles", a.require("roles.manage"), a.roles)
	auth.POST("/roles", a.require("roles.manage"), a.saveRole)
	auth.PUT("/roles/:id", a.require("roles.manage"), a.saveRole)
	auth.DELETE("/roles/:id", a.require("roles.manage"), a.deleteRole)
	auth.GET("/permissions", a.require("roles.manage"), func(c *gin.Context) { c.JSON(200, Permissions) })
	auth.GET("/settings", a.getSettings)
	auth.PUT("/settings", a.require("settings.manage"), a.saveSettings)
	auth.GET("/providers", a.require("settings.manage"), a.providers)
	auth.POST("/providers", a.require("settings.manage"), a.saveProvider)
	auth.PUT("/providers/:id", a.require("settings.manage"), a.saveProvider)
	auth.GET("/audits", a.require("audit.read"), a.audits)
	frontend := webui.Files()
	if root := os.Getenv("WEB_DIR"); root != "" {
		frontend = os.DirFS(root)
	}
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			fail(c, 404, "接口不存在")
			return
		}
		clean := path.Clean(c.Request.URL.Path)
		if strings.HasPrefix(clean, "/assets/") || strings.HasPrefix(clean, "/fonts/") {
			if info, err := fs.Stat(frontend, strings.TrimPrefix(clean, "/")); err == nil && !info.IsDir() {
				c.FileFromFS(clean, http.FS(frontend))
				return
			}
			c.Status(404)
			return
		}
		index, err := fs.ReadFile(frontend, "index.html")
		if err != nil {
			fail(c, 503, "前端文件不可用，请先构建前端或使用包含前端的发布版本")
			return
		}
		c.Data(200, "text/html; charset=utf-8", index)
	})
	return r
}
func (a *App) authenticate(c *gin.Context) {
	token, err := c.Cookie("opsdeck_session")
	if err != nil {
		fail(c, 401, "请先登录")
		return
	}
	var s Session
	var u User
	settings, e := a.settings()
	if e != nil {
		fail(c, 503, "无法读取会话策略")
		return
	}
	if a.DB.First(&s, "id = ?", TokenHash(token)).Error != nil || !SessionValid(s, settings, time.Now()) || a.DB.Preload("Roles").First(&u, s.UserID).Error != nil || !u.Enabled {
		fail(c, 401, "会话已失效，请重新登录")
		return
	}
	c.Set("user", u)
	c.Set("session", s)
	if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
		if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-CSRF-Token")), []byte(s.CSRF)) != 1 {
			fail(c, 403, "请求校验失败，请刷新页面")
			return
		}
	}
	if u.MustChangePassword && c.Request.URL.Path != "/api/me" && c.Request.URL.Path != "/api/password" && c.Request.URL.Path != "/api/logout" {
		fail(c, 403, "请先修改临时密码")
		return
	}
	c.Next()
}
func (a *App) require(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !Can(current(c), permission, nil) {
			if c.Request.Method != "GET" {
				if err := a.record(c, a.DB, permission, "", "denied", "权限不足"); err != nil {
					fail(c, 503, "审计服务不可用")
					return
				}
			}
			fail(c, 403, "没有执行此操作的权限")
			return
		}
		c.Next()
	}
}
func (a *App) record(c *gin.Context, db *gorm.DB, action, target, result, message string) error {
	row := Audit{Actor: "anonymous", SourceIP: c.ClientIP(), Action: action, Target: target, Result: result, Message: message, RequestID: c.GetString("request_id")}
	if v, ok := c.Get("user"); ok {
		u := v.(User)
		row.UserID = &u.ID
		row.Actor = u.Username
	}
	if submittedAudit(action) && row.Result == "success" {
		row.Result = "accepted"
	}
	if snapshot, ok := c.Get("audit_target_snapshot"); ok {
		value := snapshot.(Audit)
		row.TargetLabel, row.TargetDetails = value.TargetLabel, value.TargetDetails
	}
	if err := fillAuditTarget(db, &row); err != nil {
		return err
	}
	err := db.Create(&row).Error
	if err == nil {
		c.Set("audited", true)
	}
	return err
}
func (a *App) mutation(c *gin.Context, action, target string, fn func(*gorm.DB) error) bool {
	err := a.DB.Transaction(func(tx *gorm.DB) error {
		if err := fn(tx); err != nil {
			return err
		}
		return a.record(c, tx, action, target, "success", "")
	})
	if err != nil {
		if e := a.record(c, a.DB, action, target, "failed", "操作未完成"); e != nil {
			slog.Error("audit write failed")
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(c, 404, "记录不存在")
		} else {
			fail(c, 400, "操作未完成，请检查输入和关联记录")
		}
		return false
	}
	return true
}
func ValidEndpoint(s string) bool {
	u, e := url.Parse(s)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}
