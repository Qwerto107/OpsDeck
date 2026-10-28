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

type monitorTransport func(*http.Request) (*http.Response, error)

func (f monitorTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func monitorClient(t *testing.T, respond func(map[string]any) map[string]any) (*Tencent, CloudAccount) {
	t.Helper()
	block, _ := aes.NewCipher(make([]byte, 32))
	key, _ := cipher.NewGCM(block)
	id, _ := Encrypt(key, "test-id")
	secret, _ := Encrypt(key, "test-secret")
	original := http.DefaultTransport
	http.DefaultTransport = monitorTransport(func(r *http.Request) (*http.Response, error) {
		var params map[string]any
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
			return nil, err
		}
		body, _ := json.Marshal(map[string]any{"Response": respond(params)})
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	return &Tencent{Key: key}, CloudAccount{ID: 1, SecretID: id, SecretKey: secret}
}

func TestMonitorBatchChunksAndMatchesInstanceDimensions(t *testing.T) {
	calls := 0
	client, ac := monitorClient(t, func(params map[string]any) map[string]any {
		calls++
		instances := array(params, "Instances")
		if len(instances) > 50 {
			t.Errorf("批次超过腾讯云上限: %d", len(instances))
		}
		points := []any{}
		for i := len(instances) - 1; i >= 0; i-- {
			id := str(array(instances[i].(map[string]any), "Dimensions")[0].(map[string]any), "Value")
			if id == "ins-010" {
				continue
			}
			points = append(points, map[string]any{"Dimensions": []any{map[string]any{"Name": "InstanceId", "Value": id}}, "Values": []any{float64(i)}, "Timestamps": []any{float64(time.Now().Unix())}})
		}
		points = append(points, map[string]any{"Dimensions": []any{map[string]any{"Name": "InstanceId", "Value": "other-instance"}}, "Values": []any{999.0}, "Timestamps": []any{float64(time.Now().Unix())}})
		return map[string]any{"DataPoints": points, "RequestId": "test-request"}
	})
	rows := []Resource{}
	for i := 0; i < 51; i++ {
		rows = append(rows, Resource{AccountID: ac.ID, Kind: "cvm", Region: "ap-beijing", InstanceID: fmt.Sprintf("ins-%03d", i), State: "RUNNING"})
	}
	rows = append(rows, Resource{AccountID: ac.ID, Kind: "cvm", Region: "ap-beijing", InstanceID: "stopped", State: "STOPPED"})
	out := client.MonitorBatch(context.Background(), ac, rows, []string{"cpu", "disk"})
	if calls != 4 {
		t.Fatalf("51 台实例的两个指标应为 4 次请求，实际 %d", calls)
	}
	if out["ins-000"]["cpu"].Value == nil || *out["ins-000"]["cpu"].Value != 0 {
		t.Fatal("乱序响应没有正确匹配实例，或零值被丢弃")
	}
	if out["ins-010"]["cpu"].Value != nil || out["ins-010"]["cpu"].Status != "unavailable" {
		t.Fatal("缺失实例被写入其他实例的数据")
	}
	if out["stopped"]["cpu"].Value != nil || !strings.Contains(out["stopped"]["cpu"].Reason, "未运行") {
		t.Fatal("关机实例被查询监控")
	}
	if _, ok := out["other-instance"]; ok {
		t.Fatal("写入了非请求实例的响应")
	}
	if !strings.Contains(out["ins-000"]["disk"].Reason, "实例级") {
		t.Fatal("磁盘指标丢失实例级说明")
	}
}

func TestMonitorBatchFailureIsolatedByMetric(t *testing.T) {
	client, ac := monitorClient(t, func(params map[string]any) map[string]any {
		if str(params, "MetricName") == "DiskUsage" {
			return map[string]any{"Error": map[string]any{"Code": "UnauthorizedOperation", "Message": "denied"}, "RequestId": "failed"}
		}
		if str(params, "Namespace") != "QCE/LIGHTHOUSE" {
			t.Error("轻量服务器命名空间错误")
		}
		return map[string]any{"DataPoints": []any{map[string]any{"Dimensions": []any{map[string]any{"Name": "InstanceId", "Value": "lhins-test"}}, "Values": []any{12.5}, "Timestamps": []any{float64(time.Now().Unix())}}}, "RequestId": "test"}
	})
	rows := []Resource{{AccountID: ac.ID, Kind: "lighthouse", Region: "ap-guangzhou", InstanceID: "lhins-test", State: "RUNNING"}}
	out := client.MonitorBatch(context.Background(), ac, rows, []string{"cpu", "disk", "network_in"})["lhins-test"]
	if out["cpu"].Status != "ok" || out["disk"].Status != "error" || out["network_in"].Unit != "MB/s" {
		t.Fatalf("失败指标污染其他指标或单位错误: %#v", out)
	}
}
