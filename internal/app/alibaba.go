package app

import (
	"context"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	sdkerrors "github.com/aliyun/alibaba-cloud-sdk-go/sdk/errors"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
)

type Alibaba struct{ Key cipher.AEAD }
type contextTransport struct{ ctx context.Context }

func (t contextTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return http.DefaultTransport.RoundTrip(r.WithContext(t.ctx))
}

// SDK 负责签名；只返回错误码，避免把凭证和请求正文写入任务或日志。
func (a *Alibaba) call(ctx context.Context, ac CloudAccount, region, service, version, action string, params map[string]string) (map[string]any, error) {
	id, e := Decrypt(a.Key, ac.SecretID)
	if e != nil {
		return nil, &CloudError{Code: "CredentialDecryptFailed", Definite: true}
	}
	key, e := Decrypt(a.Key, ac.SecretKey)
	if e != nil {
		return nil, &CloudError{Code: "CredentialDecryptFailed", Definite: true}
	}
	config := sdk.NewConfig()
	config.Scheme = "HTTPS"
	config.Timeout = 15 * time.Second
	config.AutoRetry = false
	config.Transport = contextTransport{ctx}
	client, e := sdk.NewClientWithOptions(region, config, &credentials.AccessKeyCredential{AccessKeyId: id, AccessKeySecret: key})
	if e != nil {
		return nil, &CloudError{Code: "CloudClientFailed", Definite: true}
	}
	req := requests.NewCommonRequest()
	req.Scheme = "HTTPS"
	req.Method = "POST"
	req.Version = version
	req.ApiName = action
	req.Domain = service + "." + region + ".aliyuncs.com"
	req.QueryParams["RegionId"] = region
	for k, v := range params {
		req.QueryParams[k] = v
	}
	response, e := client.ProcessCommonRequest(req)
	if e != nil {
		var se *sdkerrors.ServerError
		if errors.As(e, &se) {
			return nil, &CloudError{Code: se.ErrorCode(), RequestID: se.RequestId(), Definite: se.HttpStatus() >= 400 && se.HttpStatus() < 500 && se.HttpStatus() != 408}
		}
		return nil, &CloudError{Code: "CloudRequestFailed", Definite: false}
	}
	var body map[string]any
	if json.Unmarshal([]byte(response.GetHttpContentString()), &body) != nil {
		return nil, &CloudError{Code: "InvalidCloudResponse"}
	}
	if success, ok := body["Success"].(bool); ok && !success {
		return nil, &CloudError{Code: str(body, "Code"), RequestID: str(body, "RequestId"), Definite: false}
	}
	return body, nil
}
func alibabaProduct(kind string) (string, string) {
	if kind == "sas" {
		return "swas", "2020-06-01"
	}
	return "ecs", "2014-05-26"
}
func (a *Alibaba) Kinds(CloudAccount) []string { return []string{"ecs", "sas", "mysql", "postgresql"} }
func (a *Alibaba) Regions(ctx context.Context, ac CloudAccount, kind string) ([]string, error) {
	service, version := alibabaProduct(kind)
	if isDatabase(kind) {
		service, version = "rds", "2014-08-15"
	}
	action := "DescribeRegions"
	if kind == "sas" {
		action = "ListRegions"
	}
	seed := "cn-hangzhou"
	if ac.Site == "international" {
		seed = "ap-southeast-1"
	}
	body, e := a.call(ctx, ac, seed, service, version, action, nil)
	if e != nil {
		return nil, e
	}
	items := array(obj(body, "Regions"), "Region")
	if kind == "sas" {
		items = array(body, "Regions")
	}
	if items == nil {
		return nil, &CloudError{Code: "InvalidRegionListResponse"}
	}
	out := []string{}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok || str(m, "RegionId") == "" {
			return nil, &CloudError{Code: "InvalidRegionListResponse"}
		}
		out = append(out, str(m, "RegionId"))
	}
	return out, nil
}
func (a *Alibaba) instances(ctx context.Context, ac CloudAccount, region, kind, id string) ([]Resource, error) {
	service, version := alibabaProduct(kind)
	action := "DescribeInstances"
	if kind == "sas" {
		action = "ListInstances"
	}
	rows := []Resource{}
	for page := 1; ; page++ {
		params := map[string]string{"PageNumber": strconv.Itoa(page), "PageSize": "100"}
		if id != "" {
			b, _ := json.Marshal([]string{id})
			params["InstanceIds"] = string(b)
		}
		body, e := a.call(ctx, ac, region, service, version, action, params)
		if e != nil {
			return nil, e
		}
		items := array(obj(body, "Instances"), "Instance")
		if kind == "sas" {
			items = array(body, "Instances")
		}
		if items == nil {
			return nil, &CloudError{Code: "InvalidInstanceListResponse"}
		}
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok || str(m, "InstanceId") == "" {
				return nil, &CloudError{Code: "InvalidInstanceListResponse"}
			}
			r := Resource{AccountID: ac.ID, Provider: ac.Provider, Kind: kind, Region: region, InstanceID: str(m, "InstanceId"), Name: str(m, "InstanceName"), State: strings.ToUpper(str(m, "Status")), Spec: str(m, "InstanceType"), CPU: num(m, "Cpu"), Memory: num(m, "Memory") / 1024, Tags: map[string]string{}, Metrics: map[string]Metric{}, SyncedAt: time.Now()}
			r.PublicIP = ips(obj(m, "PublicIpAddress"), "IpAddress")
			r.PrivateIP = ips(obj(obj(m, "VpcAttributes"), "PrivateIpAddress"), "IpAddress")
			if r.PrivateIP == "" {
				r.PrivateIP = ips(obj(m, "InnerIpAddress"), "IpAddress")
			}
			if r.PublicIP == "" {
				r.PublicIP = str(obj(m, "EipAddress"), "IpAddress")
			}
			tags := array(obj(m, "Tags"), "Tag")
			if kind == "sas" {
				spec := obj(m, "ResourceSpec")
				r.Spec = str(m, "PlanId")
				r.CPU = num(spec, "Cpu")
				r.Memory = num(spec, "Memory")
				r.Disk = num(spec, "DiskSize")
				r.PublicIP = str(m, "PublicIpAddress")
				r.PrivateIP = str(m, "InnerIpAddress")
				tags = array(m, "Tags")
			}
			key, value := "TagKey", "TagValue"
			if kind == "sas" {
				key, value = "Key", "Value"
			}
			for _, item := range tags {
				if tag, ok := item.(map[string]any); ok && str(tag, key) != "" {
					r.Tags[str(tag, key)] = str(tag, value)
				}
			}
			if _, ok := m["Tags"]; !ok {
				r.TagsError = "云接口未返回标签字段"
			}
			rows = append(rows, r)
		}
		if len(items) < 100 || id != "" {
			break
		}
	}
	return rows, nil
}
func (a *Alibaba) List(ctx context.Context, ac CloudAccount, region, kind string) ([]Resource, error) {
	if isDatabase(kind) {
		return a.listDatabases(ctx, ac, region, kind)
	}
	rows, e := a.instances(ctx, ac, region, kind, "")
	if e != nil || kind == "sas" || len(rows) == 0 {
		return rows, e
	}
	// ECS 实例接口不包含完整磁盘容量，按地域分页获取磁盘后汇总。
	sizes := map[string]int{}
	for page := 1; ; page++ {
		body, e := a.call(ctx, ac, region, "ecs", "2014-05-26", "DescribeDisks", map[string]string{"PageNumber": strconv.Itoa(page), "PageSize": "100"})
		if e != nil {
			return nil, e
		}
		disks := array(obj(body, "Disks"), "Disk")
		if disks == nil {
			return nil, &CloudError{Code: "InvalidDiskListResponse"}
		}
		for _, d := range disks {
			m, _ := d.(map[string]any)
			sizes[str(m, "InstanceId")] += num(m, "Size")
		}
		if len(disks) < 100 {
			break
		}
	}
	for i := range rows {
		rows[i].Disk = sizes[rows[i].InstanceID]
	}
	return rows, nil
}
func alibabaOperation(r Resource, action string) (string, map[string]string) {
	params := map[string]string{"InstanceId": r.InstanceID}
	api := "RebootInstance"
	if action == "start" {
		api = "StartInstance"
	} else if action == "stop" || action == "force_stop" {
		api = "StopInstance"
	}
	if r.Kind == "sas" && action != "start" {
		api += "s"
		delete(params, "InstanceId")
		b, _ := json.Marshal([]string{r.InstanceID})
		params["InstanceIds"] = string(b)
	}
	if action != "start" {
		key := "ForceStop"
		if r.Kind == "sas" && strings.Contains(action, "reboot") {
			key = "ForceReboot"
		}
		params[key] = strconv.FormatBool(strings.HasPrefix(action, "force_"))
	}
	if r.Kind == "ecs" && (action == "stop" || action == "force_stop") {
		params["StoppedMode"] = "KeepCharging"
	}
	params["ClientToken"] = RandomToken()
	return api, params
}
func (a *Alibaba) Execute(ctx context.Context, ac CloudAccount, r Resource, action string) (string, error) {
	if !isServer(r.Kind) {
		return "", &CloudError{Code: "ReadOnlyResource", Definite: true}
	}
	service, version := alibabaProduct(r.Kind)
	api, params := alibabaOperation(r, action)
	body, e := a.call(ctx, ac, r.Region, service, version, api, params)
	if e != nil {
		return "", e
	}
	return str(body, "RequestId"), nil
}
func (a *Alibaba) State(ctx context.Context, ac CloudAccount, r Resource) (CloudState, error) {
	rows, e := a.instances(ctx, ac, r.Region, r.Kind, r.InstanceID)
	if e != nil {
		return CloudState{}, e
	}
	if len(rows) != 1 {
		return CloudState{}, &CloudError{Code: "ResourceNotFound", Definite: true}
	}
	return CloudState{State: rows[0].State}, nil
}

func (a *Alibaba) MonitorBatch(ctx context.Context, ac CloudAccount, rows []Resource, enabled []string) map[string]map[string]Metric {
	out := map[string]map[string]Metric{}
	if len(rows) == 0 {
		return out
	}
	region, kind := rows[0].Region, rows[0].Kind
	namespace := "acs_ecs_dashboard"
	if kind == "sas" {
		namespace = "acs_swas"
	}
	names := map[string]string{"cpu": "CPUUtilization", "memory": "memory_usedutilization", "disk": "diskusage_utilization", "network_in": "InternetInRate", "network_out": "InternetOutRate"}
	ids := []string{}
	for _, r := range rows {
		out[r.InstanceID] = map[string]Metric{}
		for _, key := range enabled {
			unit := "%"
			if strings.HasPrefix(key, "network_") {
				unit = "bps"
			}
			out[r.InstanceID][key] = Metric{Unit: unit, Status: "unavailable", Reason: "云监控暂无数据，内存和磁盘空间指标需要云端已有采集数据"}
		}
		if r.AccountID != ac.ID || r.Region != region || r.Kind != kind {
			return out
		}
		if r.State == "RUNNING" {
			ids = append(ids, r.InstanceID)
		}
	}
	if len(ids) == 0 || len(enabled) == 0 {
		return out
	}
	units := map[string]string{}
	periods := map[string]string{}
	if kind == "sas" {
		// 轻量产品按云端指标元数据匹配，避免套用 ECS 的名称或单位。
		candidates := map[string][]string{
			"cpu":         {"CPUUtilization", "CpuUtilization", "cpu_total"},
			"memory":      {"memory_usedutilization", "MemoryUtilization"},
			"disk":        {"diskusage_utilization", "DiskUtilization"},
			"network_in":  {"InternetInRate", "InternetRX", "NetInRate"},
			"network_out": {"InternetOutRate", "InternetTX", "NetOutRate"},
		}
		names = map[string]string{}
		for page := 1; ; page++ {
			body, err := a.call(ctx, ac, region, "metrics", "2019-01-01", "DescribeMetricMetaList", map[string]string{"Namespace": namespace, "PageNumber": strconv.Itoa(page), "PageSize": "100"})
			if err != nil {
				for _, metrics := range out {
					for key, m := range metrics {
						m.Status = "error"
						m.Reason = "无法读取轻量云监控指标：" + err.Error()
						metrics[key] = m
					}
				}
				return out
			}
			items := array(obj(body, "Resources"), "Resource")
			for _, item := range items {
				meta, _ := item.(map[string]any)
				for key, aliases := range candidates {
					for _, alias := range aliases {
						if strings.EqualFold(str(meta, "MetricName"), alias) && names[key] == "" {
							names[key] = str(meta, "MetricName")
							units[key] = str(meta, "Unit")
							periods[key] = strings.Split(str(meta, "Periods"), ",")[0]
						}
					}
				}
			}
			if len(items) < 100 {
				break
			}
		}
	}
	end := time.Now().UTC()
	for start := 0; start < len(ids); start += 50 {
		batch := ids[start:min(start+50, len(ids))]
		dims := []map[string]string{}
		for _, id := range batch {
			dims = append(dims, map[string]string{"instanceId": id})
		}
		encoded, _ := json.Marshal(dims)
		for _, key := range enabled {
			if names[key] == "" {
				continue
			}
			token := ""
			seen := map[string]bool{}
			for {
				period := periods[key]
				if period == "" {
					period = "60"
				}
				params := map[string]string{"Namespace": namespace, "MetricName": names[key], "Dimensions": string(encoded), "Period": period, "StartTime": strconv.FormatInt(end.Add(-5*time.Minute).UnixMilli(), 10), "EndTime": strconv.FormatInt(end.UnixMilli(), 10), "Length": "1000"}
				if token != "" {
					params["NextToken"] = token
				}
				body, e := a.call(ctx, ac, region, "metrics", "2019-01-01", "DescribeMetricList", params)
				if e != nil {
					for _, id := range batch {
						m := out[id][key]
						m.Status = "error"
						m.Reason = "云监控请求失败：" + e.Error()
						out[id][key] = m
					}
					break
				}
				var points []map[string]any
				if json.Unmarshal([]byte(str(body, "Datapoints")), &points) != nil {
					break
				}
				for _, dp := range points {
					id := str(dp, "instanceId")
					valid := false
					for _, requested := range batch {
						valid = valid || requested == id
					}
					if !valid {
						continue
					}
					v, ok := dp["Average"].(float64)
					ts, okTS := dp["timestamp"].(float64)
					if !ok || !okTS {
						continue
					}
					sampled := time.UnixMilli(int64(ts))
					m := out[id][key]
					if units[key] != "" {
						m.Unit = units[key]
					}
					if m.SampledAt != nil && sampled.Before(*m.SampledAt) {
						continue
					}
					if key == "disk" && m.SampledAt != nil && sampled.Equal(*m.SampledAt) && m.Value != nil && v < *m.Value {
						continue
					}
					m.Value = &v
					m.SampledAt = &sampled
					m.Status = "ok"
					m.Reason = ""
					if key == "disk" {
						m.Reason = fmt.Sprintf("云监控挂载点最高空间使用率（%s）", str(dp, "device"))
					}
					out[id][key] = m
				}
				token = str(body, "NextToken")
				if token == "" || seen[token] {
					break
				}
				seen[token] = true
			}
		}
	}
	return out
}
