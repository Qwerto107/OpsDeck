package app

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

func validAppURL(value string) bool {
	if value == "" {
		return true
	}
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && !strings.Contains(value, "#")
}

func requestOrigin(c *gin.Context) string {
	// 未配置固定地址时只接受当前 Host 对应的浏览器来源，不信任转发头推导域名。
	if value := c.GetHeader("Origin"); validAppURL(value) && value != "" {
		u, _ := url.Parse(value)
		if strings.EqualFold(u.Host, c.Request.Host) {
			return value
		}
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

func appURL(c *gin.Context) string     { return c.GetString("app_url") }
func secureCookie(c *gin.Context) bool { return c.GetBool("secure_cookie") }
func sourceIP(c *gin.Context) string   { return c.GetString("source_ip") }
