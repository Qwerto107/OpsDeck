package app

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTencentDatabaseMetricsUseProductDimensions(t *testing.T) {
	for _, test := range []struct{ kind, prefix, namespace, dimension string }{
		{"mysql", "", "QCE/CDB", "InstanceId"},
		{"mysql", "Cluster", "QCE/CDB", "InstanceId"},
		{"postgresql", "", "QCE/POSTGRES", "resourceId"},
	} {
		t.Run(test.kind+test.prefix, func(t *testing.T) {
			calls := 0
			client, account := monitorClient(t, func(params map[string]any) map[string]any {
				calls++
				metric := str(params, "MetricName")
				if str(params, "Namespace") != test.namespace || !strings.HasPrefix(metric, test.prefix) {
					t.Fatal("数据库使用了错误的监控命名空间或架构指标")
				}
				dimension := array(array(params, "Instances")[0].(map[string]any), "Dimensions")[0].(map[string]any)
				if str(dimension, "Name") != test.dimension {
					t.Fatal("数据库监控维度错误")
				}
				return map[string]any{"DataPoints": []any{map[string]any{"Dimensions": []any{dimension}, "Values": []any{0.0}, "Timestamps": []any{float64(time.Now().Unix())}}}}
			})
			row := Resource{AccountID: account.ID, Region: "ap-guangzhou", Kind: test.kind, InstanceID: "db-test", State: "RUNNING", MonitorPrefix: test.prefix}
			metrics := client.MonitorBatch(context.Background(), account, []Resource{row}, []string{"cpu", "memory", "disk", "connections"})[row.InstanceID]
			if calls != 4 || len(metrics) != 4 || metrics["connections"].Unit != "个" {
				t.Fatal("数据库指标数量或连接数单位错误")
			}
			for _, metric := range metrics {
				if metric.Status != "ok" || metric.Value == nil || *metric.Value != 0 {
					t.Fatal("数据库零值被丢弃")
				}
			}
		})
	}
}

func TestAlibabaDatabaseMetricsParseFormatsAndStorage(t *testing.T) {
	for _, kind := range []string{"mysql", "postgresql"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			client, account := alibabaFixture(t, func(request *http.Request) map[string]any {
				calls++
				if request.URL.Query().Get("Action") != "DescribeDBInstancePerformance" || request.URL.Query().Get("DBInstanceId") != "db-test" {
					t.Fatal("数据库监控请求错误")
				}
				entry := func(key, unit, format, value string) map[string]any {
					return map[string]any{"Key": key, "Unit": unit, "ValueFormat": format, "Values": map[string]any{"PerformanceValue": []any{
						map[string]any{"Date": "2026-10-28T15:00:00Z", "Value": value},
						map[string]any{"Date": "2026-10-28T14:55:00Z", "Value": "99"},
					}}}
				}
				entries := []any{entry("MySQL_MemCpuUsage", "%", "memusage&cpuusage", "25&0"), entry("MySQL_DetailedSpaceUsage", "MB", "ins_size&data_size", "1024&512"), entry("MySQL_Sessions", "int", "active_session&total_session", "3&8")}
				if kind == "postgresql" {
					entries = []any{entry("CpuUsage", "%", "cpu", "0"), entry("MemoryUsage", "%", "memory", "25"), entry("PgSQL_SpaceUsage", "byte", "space", "1073741824"), entry("PgSQL_Session", "int", "connections", "8")}
				}
				return map[string]any{"DBInstanceId": "db-test", "PerformanceKeys": map[string]any{"PerformanceKey": entries}}
			})
			row := Resource{AccountID: account.ID, Region: "cn-hangzhou", Kind: kind, InstanceID: "db-test", State: "RUNNING", Disk: 4}
			metrics := client.MonitorBatch(context.Background(), account, []Resource{row}, nil)[row.InstanceID]
			for key, want := range map[string]float64{"cpu": 0, "memory": 25, "disk": 25, "connections": 8} {
				if metric := metrics[key]; metric.Value == nil || *metric.Value != want {
					t.Fatalf("%s 的格式、最新采样或单位换算错误: %+v", key, metric)
				}
			}
			if calls != 1 {
				t.Fatal("同一实例的指标没有合并查询")
			}
			if key, _ := rdsMetric("PgSQL_SpaceUsage", "space", "unknown", 4); key != "" {
				t.Fatal("未知存储单位被当作使用率")
			}
		})
	}
}
