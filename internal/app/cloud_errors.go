package app

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"regexp"
	"strings"
)

type cloudTaskContextKey struct{}

var cloudSecretPattern = regexp.MustCompile(`(?i)((?:accesskey(?:id|secret)?|secret(?:id|key)?|securitytoken|authorization|signature|token)[\s"':=]+)[^\s"',;&]+`)

func sanitizeCloudMessage(message string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
			message = strings.ReplaceAll(message, url.QueryEscape(secret), "[REDACTED]")
		}
	}
	message = cloudSecretPattern.ReplaceAllString(message, "${1}[REDACTED]")
	message = strings.Join(strings.Fields(message), " ")
	runes := []rune(message)
	if len(runes) > 1500 {
		message = string(runes[:1500]) + "…"
	}
	return message
}

func cloudPermission(provider, service, action string) string {
	if provider == "alibaba" {
		if service == "swas" {
			if action == "ListRegions" {
				return ""
			}
			return "swas-open:" + action
		}
		if service == "metrics" {
			if action == "DescribeMetricList" {
				return "cms:QueryMetricList"
			}
			return "cms:" + action
		}
	}
	// 地域查询 API 名称不一定是可授权 Action，不能提示用户添加无效策略。
	if service == "lighthouse" && action == "DescribeRegions" {
		return ""
	}
	return service + ":" + action
}

func (e *CloudError) Error() string {
	parts := []string{e.Code}
	if e.Service != "" {
		parts = append(parts, "接口 "+e.Service+"/"+e.Action)
	}
	if e.Message != "" {
		parts = append(parts, "云端说明："+e.Message)
	}
	code := strings.ToLower(e.Code)
	if strings.Contains(code, "forbidden") || strings.Contains(code, "unauthor") || strings.Contains(code, "accessdenied") || code == "403" {
		if e.Permission != "" {
			parts = append(parts, "权限排查："+e.Permission+"，同时检查资源范围、条件与显式拒绝")
		} else {
			parts = append(parts, "请核对该接口的云端鉴权及账号限制，勿直接将 API 名称添加为策略 Action")
		}
	}
	if strings.Contains(code, "throttl") || strings.Contains(code, "requestlimit") {
		parts = append(parts, "云接口限流，请稍后重试或降低请求频率")
	}
	if strings.Contains(code, "invalidaccesskey") || strings.Contains(code, "secretidnotfound") {
		parts = append(parts, "凭证不存在或无效，请核对密钥是否启用、填写正确并属于当前账号及站点")
	} else if strings.Contains(code, "authfailure") || strings.Contains(code, "signature") {
		parts = append(parts, "请核对云凭证、账号站点及服务器时间")
	}
	if e.RequestID != "" {
		parts = append(parts, "Request ID "+e.RequestID)
	}
	return strings.Join(parts, "；")
}

func networkCloudError(err error) *CloudError {
	if errors.Is(err, context.DeadlineExceeded) {
		return &CloudError{Code: "CloudRequestTimeout", Message: "请求超时，请检查云接口响应、网络及代理"}
	}
	if errors.Is(err, context.Canceled) {
		return &CloudError{Code: "CloudRequestCanceled", Message: "任务已取消或服务正在停止"}
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return &CloudError{Code: "CloudDNSFailed", Message: "云接口域名解析失败，请检查地域端点、DNS 和网络"}
	}
	var certificate x509.UnknownAuthorityError
	if errors.As(err, &certificate) {
		return &CloudError{Code: "CloudTLSFailed", Message: "云接口证书验证失败，请检查证书信任链及代理"}
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return &CloudError{Code: "CloudRequestTimeout", Message: "连接或读取云接口超时，请检查网络及代理"}
	}
	return &CloudError{Code: "CloudRequestFailed", Message: "云接口连接失败，请检查地域端点、网络和代理状态"}
}

func reportCloudError(ctx context.Context, ac CloudAccount, region, service, action string, err error) error {
	if err == nil {
		return nil
	}
	var failure *CloudError
	if !errors.As(err, &failure) {
		failure = &CloudError{Code: "CloudClientFailed", Message: "云接口客户端处理失败"}
	}
	failure.Service, failure.Action = service, action
	if failure.Code == "" {
		failure.Code = "CloudAPIError"
	}
	failure.Permission = cloudPermission(ac.Provider, service, action)
	slog.ErrorContext(ctx, "cloud api request failed", "task_id", ctx.Value(cloudTaskContextKey{}), "account_id", ac.ID, "provider", ac.Provider, "site", ac.Site, "region", region, "service", service, "action", action, "error_code", failure.Code, "permission", failure.Permission, "request_id", failure.RequestID, "message", failure.Message)
	return failure
}

func syncFailure(stage, kind, region string, err error) string {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts := []string{}
		for _, item := range joined.Unwrap() {
			parts = append(parts, syncFailure(stage, kind, region, item))
		}
		return strings.Join(parts, "\n")
	}
	reason := "内部处理失败，请检查服务错误日志"
	var cloud *CloudError
	if errors.As(err, &cloud) {
		reason = cloud.Error()
	} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		reason = networkCloudError(err).Error()
	}
	if region == "" {
		region = "地域发现入口"
	}
	return fmt.Sprintf("[%s] %s / %s：%s", stage, kind, region, reason)
}
