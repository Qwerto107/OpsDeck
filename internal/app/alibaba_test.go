package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type alibabaTestTransport func(*http.Request) (*http.Response, error)

func (f alibabaTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func alibabaFixture(t *testing.T, handle func(*http.Request) map[string]any) (*Alibaba, CloudAccount) {
	t.Helper()
	block, err := aes.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	key, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	id, e := Encrypt(key, "test-key-id")
	if e != nil {
		t.Fatal(e)
	}
	secret, e := Encrypt(key, "test-secret")
	if e != nil {
		t.Fatal(e)
	}
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = alibabaTestTransport(func(r *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(handle(r))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	return &Alibaba{Key: key}, CloudAccount{ID: 1, Provider: "alibaba", Site: "international", SecretID: id, SecretKey: secret}
}
func TestAlibabaBatchMatchesInstancesAndMounts(t *testing.T) {
	calls := 0
	now := time.Now().UnixMilli()
	a, ac := alibabaFixture(t, func(r *http.Request) map[string]any {
		if r.URL.Host != "metrics.ap-southeast-1.aliyuncs.com" || r.URL.Scheme != "https" {
			t.Fatalf("监控端点错误: %s", r.URL.Host)
		}
		if r.URL.Query().Get("Action") != "DescribeMetricList" {
			t.Fatal("不应调用操作接口")
		}
		var dims []map[string]string
		if e := json.Unmarshal([]byte(r.URL.Query().Get("Dimensions")), &dims); e != nil {
			t.Fatal(e)
		}
		if len(dims) > 50 {
			t.Fatal("批量查询超过上限")
		}
		calls++
		points := []map[string]any{}
		for i := len(dims) - 1; i >= 0; i-- {
			points = append(points, map[string]any{"instanceId": dims[i]["instanceId"], "timestamp": now, "Average": 80, "device": "/data"}, map[string]any{"instanceId": dims[i]["instanceId"], "timestamp": now, "Average": 25, "device": "/"})
		}
		points = append(points, map[string]any{"instanceId": "outside", "timestamp": now, "Average": 99})
		b, _ := json.Marshal(points)
		return map[string]any{"Success": true, "Datapoints": string(b)}
	})
	rows := []Resource{}
	for i := 0; i < 51; i++ {
		rows = append(rows, Resource{AccountID: ac.ID, Kind: "ecs", Region: "ap-southeast-1", InstanceID: fmt.Sprint(i), State: "RUNNING"})
	}
	out := a.MonitorBatch(context.Background(), ac, rows, []string{"disk"})
	if calls != 2 {
		t.Fatalf("51 台实例应拆成 2 次请求，实际 %d", calls)
	}
	for _, row := range rows {
		m := out[row.InstanceID]["disk"]
		if m.Value == nil || *m.Value != 80 {
			t.Fatal("实例或磁盘挂载点匹配错误")
		}
	}
	if _, ok := out["outside"]; ok {
		t.Fatal("混入其他实例数据")
	}
}
func TestAlibabaListAndOperationParameters(t *testing.T) {
	a, ac := alibabaFixture(t, func(r *http.Request) map[string]any {
		switch r.URL.Query().Get("Action") {
		case "DescribeInstances":
			return map[string]any{"Instances": map[string]any{"Instance": []any{map[string]any{"InstanceId": "i-test", "InstanceName": "测试", "Status": "Running", "Cpu": 2, "Memory": 4096, "VpcAttributes": map[string]any{"PrivateIpAddress": map[string]any{"IpAddress": []string{"10.0.0.1"}}}}}}}
		case "DescribeDisks":
			return map[string]any{"Disks": map[string]any{"Disk": []any{map[string]any{"InstanceId": "i-test", "Size": 40}, map[string]any{"InstanceId": "i-test", "Size": 80}}}}
		}
		t.Fatalf("未知接口: %s", r.URL.Query().Get("Action"))
		return nil
	})
	rows, e := a.List(context.Background(), ac, "ap-southeast-1", "ecs")
	if e != nil || len(rows) != 1 {
		t.Fatalf("%v %v", rows, e)
	}
	r := rows[0]
	if r.Memory != 4 || r.Disk != 120 || r.State != "RUNNING" || r.PrivateIP != "10.0.0.1" {
		t.Fatalf("实例配置解析错误: %+v", r)
	}
	for _, kind := range []string{"ecs", "sas"} {
		for _, action := range []string{"start", "stop", "reboot", "force_stop", "force_reboot"} {
			api, p := alibabaOperation(Resource{Kind: kind, InstanceID: "i-test"}, action)
			if p["ClientToken"] == "" {
				t.Fatal("缺少幂等键")
			}
			if kind == "ecs" && action == "stop" && p["StoppedMode"] != "KeepCharging" {
				t.Fatal("关机不应自动释放计算资源")
			}
			if kind == "sas" && action == "force_reboot" && (api != "RebootInstances" || p["ForceReboot"] != "true") {
				t.Fatal("轻量强制重启参数错误")
			}
			if action == "reboot" && p["ForceStop"] == "true" {
				t.Fatal("普通操作不能强制执行")
			}
		}
	}
}

func TestTencentInternationalEndpoints(t *testing.T) {
	client, ac := monitorClient(t, func(map[string]any) map[string]any { return map[string]any{"RequestId": "test"} })
	ac.Site = "international"
	original := http.DefaultTransport
	http.DefaultTransport = alibabaTestTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Host, ".intl.tencentcloudapi.com") {
			t.Fatalf("国际站请求端点错误: %s", r.URL.Host)
		}
		return original.RoundTrip(r)
	})
	for _, service := range []string{"cvm", "lighthouse", "region", "monitor"} {
		if _, err := client.call(context.Background(), ac, "ap-singapore", service, "2020-01-01", "Test", map[string]any{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPostgresCloudAccountSiteCannotChange(t *testing.T) {
	a, admin := integrationApp(t)
	cookie, csrf := loginTest(t, a, admin.Username)
	requireCode(t, sendRequest(a, "POST", "/api/accounts", map[string]any{"name": "阿里国际站", "provider": "alibaba", "site": "international", "secret_id": "test-id", "secret_key": "test-secret", "enabled": false}, cookie, csrf), 200)
	var ac CloudAccount
	if err := a.DB.Where("name = ?", "阿里国际站").First(&ac).Error; err != nil {
		t.Fatal(err)
	}
	if ac.Provider != "alibaba" || ac.Site != "international" || ac.SecretID == "test-id" {
		t.Fatal("账号站点或凭证存储错误")
	}
	path := "/api/accounts/" + fmtID(ac.ID)
	requireCode(t, sendRequest(a, "PUT", path, map[string]any{"name": ac.Name, "provider": "tencent", "site": "international", "enabled": false}, cookie, csrf), 400)
	requireCode(t, sendRequest(a, "PUT", path, map[string]any{"name": ac.Name, "provider": "alibaba", "site": "china", "enabled": false}, cookie, csrf), 400)
	requireCode(t, sendRequest(a, "PUT", path, map[string]any{"name": ac.Name, "enabled": false}, cookie, csrf), 200)
	requireCode(t, sendRequest(a, "POST", "/api/accounts", map[string]any{"name": "未知厂商", "provider": "unknown", "site": "international"}, cookie, csrf), 400)
}
