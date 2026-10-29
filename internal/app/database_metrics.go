package app

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"
)

func (a *Alibaba) monitorDatabases(ctx context.Context, ac CloudAccount, rows []Resource) map[string]map[string]Metric {
	out := map[string]map[string]Metric{}
	end := time.Now().UTC()
	for _, r := range rows {
		metrics := map[string]Metric{}
		for _, key := range []string{"cpu", "memory", "disk", "connections"} {
			unit := "%"
			if key == "connections" {
				unit = "个"
			}
			metrics[key] = Metric{Unit: unit, Status: "unavailable", Reason: "云端暂无该数据库指标，请检查监控可用条件"}
		}
		out[r.InstanceID] = metrics
		if r.AccountID != ac.ID || !isDatabase(r.Kind) || r.Region != rows[0].Region || r.Kind != rows[0].Kind {
			for key, metric := range metrics {
				metric.Status, metric.Reason = "error", "监控请求混入其他账号、地域或资源类型"
				metrics[key] = metric
			}
			continue
		}
		if r.State != "RUNNING" {
			for key, metric := range metrics {
				metric.Reason = "实例未运行，无当前监控数据"
				metrics[key] = metric
			}
			continue
		}
		keys := "MySQL_MemCpuUsage,MySQL_DetailedSpaceUsage,MySQL_Sessions"
		if r.Kind == "postgresql" {
			keys = "CpuUsage,MemoryUsage,PgSQL_SpaceUsage,PgSQL_Session"
		}
		// RDS 性能接口支持一次查询多个指标，但不支持跨实例批量查询。
		body, err := a.call(ctx, ac, r.Region, "rds", "2014-08-15", "DescribeDBInstancePerformance", map[string]string{
			"DBInstanceId": r.InstanceID, "Key": keys,
			"StartTime": end.Add(-10 * time.Minute).Format("2006-01-02T15:04Z"),
			"EndTime":   end.Format("2006-01-02T15:04Z"),
		})
		if err != nil || str(body, "DBInstanceId") != r.InstanceID {
			reason := "数据库监控响应与请求实例不匹配"
			if err != nil {
				reason = "数据库监控请求失败：" + err.Error()
			}
			for key, metric := range metrics {
				metric.Status, metric.Reason = "error", reason
				metrics[key] = metric
			}
			continue
		}
		for _, item := range array(obj(body, "PerformanceKeys"), "PerformanceKey") {
			entry, _ := item.(map[string]any)
			for _, point := range array(obj(entry, "Values"), "PerformanceValue") {
				dp, _ := point.(map[string]any)
				sampled, err := time.Parse(time.RFC3339, str(dp, "Date"))
				if err != nil {
					continue
				}
				values := strings.Split(str(dp, "Value"), "&")
				formats := strings.Split(strings.ToLower(str(entry, "ValueFormat")), "&")
				if len(values) != len(formats) {
					continue
				}
				for index, text := range values {
					key, divisor := rdsMetric(str(entry, "Key"), strings.TrimSpace(formats[index]), str(entry, "Unit"), r.Disk)
					value, err := strconv.ParseFloat(text, 64)
					if key == "" || divisor <= 0 || err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
						continue
					}
					metric := metrics[key]
					if metric.SampledAt != nil && !sampled.After(*metric.SampledAt) {
						continue
					}
					value /= divisor
					metric.Value, metric.SampledAt, metric.Status, metric.Reason = &value, &sampled, "ok", ""
					if key == "disk" {
						metric.Reason = "实例已用空间 / 配置存储容量"
					}
					metrics[key] = metric
				}
			}
		}
	}
	return out
}

func rdsMetric(key, format, unit string, capacityGB int) (string, float64) {
	switch key {
	case "MySQL_MemCpuUsage":
		if format == "cpuusage" {
			return "cpu", 1
		}
		if format == "memusage" {
			return "memory", 1
		}
	case "CpuUsage":
		return "cpu", 1
	case "MemoryUsage":
		return "memory", 1
	case "MySQL_Sessions":
		if format == "total_session" {
			return "connections", 1
		}
	case "PgSQL_Session":
		return "connections", 1
	case "MySQL_DetailedSpaceUsage", "PgSQL_SpaceUsage":
		if capacityGB <= 0 || (key == "MySQL_DetailedSpaceUsage" && format != "ins_size") {
			return "", 0
		}
		// 仅在单位明确且容量已知时计算比例，避免把已用空间误当作百分比。
		switch strings.ToLower(unit) {
		case "mb", "mbytes":
			return "disk", float64(capacityGB) * 1024 / 100
		case "byte", "bytes", "b":
			return "disk", float64(capacityGB) * 1024 * 1024 * 1024 / 100
		case "gb", "gbytes":
			return "disk", float64(capacityGB) / 100
		}
	}
	return "", 0
}
