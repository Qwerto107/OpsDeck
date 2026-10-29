package app

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func captureCloudLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &output
}

func TestCloudFailuresIncludeContextAndRedactSecrets(t *testing.T) {
	for _, provider := range []string{"tencent", "alibaba"} {
		t.Run(provider, func(t *testing.T) {
			logs := captureCloudLog(t)
			var err error
			if provider == "alibaba" {
				client, ac := alibabaFixture(t, func(*http.Request) map[string]any {
					return map[string]any{"Success": false, "Code": "Forbidden.RAM", "Message": "Denied test-key-id test-secret; token=private-token Signature=private-signature", "RequestId": "request-test"}
				})
				_, err = client.call(context.Background(), ac, "ap-southeast-1", "metrics", "2019-01-01", "DescribeMetricList", nil)
			} else {
				client, ac := monitorClient(t, func(map[string]any) map[string]any {
					return map[string]any{"Error": map[string]any{"Code": "UnauthorizedOperation", "Message": "Denied test-id test-secret; token=private-token Signature=private-signature"}, "RequestId": "request-test"}
				})
				_, err = client.call(context.Background(), ac, "ap-singapore", "cvm", "2017-03-12", "DescribeInstances", nil)
			}
			if err == nil {
				t.Fatal("失败响应未返回错误")
			}
			combined := logs.String() + err.Error()
			for _, secret := range []string{"test-key-id", "test-id", "test-secret", "private-token", "private-signature"} {
				if strings.Contains(combined, secret) {
					t.Fatal("错误或日志泄露测试凭证")
				}
			}
			permission := "cvm:DescribeInstances"
			if provider == "alibaba" {
				permission = "cms:QueryMetricList"
			}
			if !strings.Contains(err.Error(), permission) || !strings.Contains(err.Error(), "request-test") || !strings.Contains(logs.String(), `"level":"ERROR"`) || !strings.Contains(logs.String(), `"account_id":1`) {
				t.Fatal("错误详情或服务错误日志缺少接口、权限、请求标识和账号")
			}
		})
	}
	if cloudPermission("alibaba", "swas", "ListRegions") != "" || cloudPermission("tencent", "lighthouse", "DescribeRegions") != "" {
		t.Fatal("生成了不可直接授权的 Action")
	}
}

func TestCloudNetworkFailuresHaveSpecificSafeReasons(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{context.DeadlineExceeded, "CloudRequestTimeout"},
		{context.Canceled, "CloudRequestCanceled"},
		{&net.DNSError{Err: "no such host", Name: "example.invalid"}, "CloudDNSFailed"},
	} {
		failure := networkCloudError(test.err)
		if failure.Code != test.code || failure.Message == "" {
			t.Fatal("网络失败原因过于笼统")
		}
	}
}

type syncErrorCloud struct{ fakeCloud }

func (*syncErrorCloud) Kinds(CloudAccount) []string { return []string{"ecs", "sas"} }
func (*syncErrorCloud) Regions(_ context.Context, _ CloudAccount, kind string) ([]string, error) {
	if kind == "ecs" {
		return nil, &CloudError{Code: "Forbidden.RAM", Service: "ecs", Action: "DescribeRegions", Permission: "ecs:DescribeRegions", Message: "not authorized", RequestID: "region-request"}
	}
	return []string{"ap-test"}, nil
}
func (*syncErrorCloud) List(context.Context, CloudAccount, string, string) ([]Resource, error) {
	return nil, &CloudError{Code: "CloudDNSFailed", Service: "swas", Action: "ListInstances", Message: "云接口域名解析失败"}
}

func TestPostgresAccountSyncRetainsEachErrorAndLogsFailures(t *testing.T) {
	a, admin := integrationApp(t)
	logs := captureCloudLog(t)
	ac := CloudAccount{Name: "sync-errors", Provider: "alibaba", Site: "international", Enabled: true, SecretID: "encrypted", SecretKey: "encrypted", Regions: []string{}}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	task := Task{AccountID: &ac.ID, Action: "sync", Status: "queued", NextRun: time.Now()}
	if err := a.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	a.Cloud = &syncErrorCloud{}
	a.process(context.Background(), task)
	a.DB.First(&task, task.ID)
	a.DB.First(&ac, ac.ID)
	if task.Status != "failed" || task.Error != ac.SyncError || len(strings.Split(task.Error, "\n")) != 2 {
		t.Fatal("没有保留全部同步失败详情")
	}
	for _, field := range []string{"地域发现", "ecs:DescribeRegions", "region-request", "资源同步", "ap-test", "CloudDNSFailed"} {
		if !strings.Contains(task.Error, field) || !strings.Contains(logs.String(), field) {
			t.Fatalf("同步详情或服务日志缺少 %s", field)
		}
	}
	viewer := User{Username: "log-viewer", Enabled: true, PasswordHash: admin.PasswordHash, Preferences: map[string]any{}, Roles: []Role{{Name: "instance-viewer", Permissions: []string{"resources.read"}, Scopes: []Scope{{AccountID: ac.ID, Kind: "ecs", InstanceID: "ins-only"}}}}}
	if err := a.DB.Create(&viewer).Error; err != nil {
		t.Fatal(err)
	}
	cookie, _ := loginTest(t, a, viewer.Username)
	for _, path := range []string{"/api/accounts", "/api/sync-tasks?ids=" + fmtID(task.ID)} {
		w := sendRequest(a, "GET", path, nil, cookie, "")
		requireCode(t, w, 200)
		if strings.Contains(w.Body.String(), "region-request") || strings.Contains(w.Body.String(), "CloudDNSFailed") {
			t.Fatal("账号日志泄露了资源范围之外的详情")
		}
	}
}

func TestPostgresAccountSyncKeepsMetricsOnMonitoringError(t *testing.T) {
	a, _ := integrationApp(t)
	ac := CloudAccount{Name: "monitor-error", Enabled: true, SecretID: "encrypted", SecretKey: "encrypted", Regions: []string{}}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	value, sampled := 17.0, time.Now().Add(-time.Minute)
	r := Resource{AccountID: ac.ID, InstanceID: "ins-failed", Kind: "cvm", Region: "ap-test", State: "RUNNING", Tags: map[string]string{}, Metrics: map[string]Metric{"cpu": {Value: &value, SampledAt: &sampled, Status: "ok"}}}
	if err := a.DB.Create(&r).Error; err != nil {
		t.Fatal(err)
	}
	a.Cloud = &refreshTestCloud{rows: []Resource{r}, failed: r.InstanceID}
	err := a.syncRegion(context.Background(), ac, r.Region, r.Kind, []string{"cpu"})
	if err == nil || !strings.Contains(err.Error(), "UnauthorizedOperation") {
		t.Fatal("监控失败没有计入账号同步失败")
	}
	a.DB.First(&r, r.ID)
	if r.Metrics["cpu"].Value == nil || *r.Metrics["cpu"].Value != value || r.Metrics["cpu"].Status != "stale" {
		t.Fatal("监控失败清空了旧采样")
	}
}
