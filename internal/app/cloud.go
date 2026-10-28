package app

import (
	"context"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	sdk "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	sdkhttp "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/http"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
)

type CloudState struct{ State, OperationState, RequestID string }
type Cloud interface {
	Regions(context.Context, CloudAccount, string) ([]string, error)
	List(context.Context, CloudAccount, string, string) ([]Resource, error)
	Execute(context.Context, CloudAccount, Resource, string) (string, error)
	State(context.Context, CloudAccount, Resource) (CloudState, error)
	Kinds(CloudAccount) []string
	MonitorBatch(context.Context, CloudAccount, []Resource, []string) map[string]map[string]Metric
}
type CloudError struct {
	Code, RequestID string
	Definite        bool
}

func (e *CloudError) Error() string { return e.Code }

type Tencent struct{ Key cipher.AEAD }

func (t *Tencent) Regions(ctx context.Context, ac CloudAccount, kind string) ([]string, error) {
	service, version := "region", "2022-06-27"
	params := map[string]any{"Product": "cvm"}
	if kind == "lighthouse" {
		service, version = cloudProduct(kind)
		params = map[string]any{}
	}
	body, err := t.call(ctx, ac, "", service, version, "DescribeRegions", params)
	if err != nil {
		return nil, err
	}
	return availableRegions(body)
}

func availableRegions(body map[string]any) ([]string, error) {
	items, ok := body["RegionSet"].([]any)
	if !ok {
		return nil, &CloudError{Code: "InvalidRegionListResponse", Definite: false}
	}
	regions := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok || str(m, "Region") == "" || str(m, "RegionState") == "" {
			return nil, &CloudError{Code: "InvalidRegionListResponse", Definite: false}
		}
		region := str(m, "Region")
		if str(m, "RegionState") == "AVAILABLE" && !seen[region] {
			regions = append(regions, region)
			seen[region] = true
		}
	}
	return regions, nil
}

func (t *Tencent) call(ctx context.Context, ac CloudAccount, region, service, version, action string, params map[string]any) (map[string]any, error) {
	id, e := Decrypt(t.Key, ac.SecretID)
	if e != nil {
		return nil, &CloudError{Code: "CredentialDecryptFailed", Definite: true}
	}
	key, e := Decrypt(t.Key, ac.SecretKey)
	if e != nil {
		return nil, &CloudError{Code: "CredentialDecryptFailed", Definite: true}
	}
	cp := profile.NewClientProfile()
	if ac.Site == "international" {
		cp.HttpProfile.Endpoint = service + ".intl.tencentcloudapi.com"
	}
	cp.HttpProfile.ReqTimeout = 15
	cp.NetworkFailureMaxRetries = 0
	cp.RateLimitExceededMaxRetries = 0
	cp.UnsafeRetryOnConnectionFailure = false
	cp.DisableRegionBreaker = true
	client := sdk.NewCommonClient(sdk.NewCredential(id, key), region, cp)
	request := sdkhttp.NewCommonRequest(service, version, action)
	request.SetContext(ctx)
	if e = request.SetActionParameters(params); e != nil {
		return nil, e
	}
	response := sdkhttp.NewCommonResponse()
	if e = client.Send(request, response); e != nil {
		var ce *sdkerrors.TencentCloudSDKError
		if errors.As(e, &ce) {
			return nil, &CloudError{Code: ce.GetCode(), RequestID: ce.GetRequestId(), Definite: !strings.HasPrefix(ce.GetCode(), "ClientError.")}
		}
		return nil, &CloudError{Code: "CloudRequestFailed", Definite: false}
	}
	var body struct{ Response map[string]any }
	if json.Unmarshal(response.GetBody(), &body) != nil || body.Response == nil {
		return nil, &CloudError{Code: "InvalidCloudResponse", Definite: false}
	}
	return body.Response, nil
}
func cloudProduct(kind string) (string, string) {
	if kind == "lighthouse" {
		return "lighthouse", "2020-03-24"
	}
	return "cvm", "2017-03-12"
}
func str(m map[string]any, k string) string         { v, _ := m[k].(string); return v }
func num(m map[string]any, k string) int            { v, _ := m[k].(float64); return int(v) }
func obj(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }
func array(m map[string]any, k string) []any        { v, _ := m[k].([]any); return v }
func ips(m map[string]any, k string) string {
	v := array(m, k)
	s := []string{}
	for _, ip := range v {
		if p, ok := ip.(string); ok {
			s = append(s, p)
		}
	}
	return strings.Join(s, ", ")
}
func (t *Tencent) List(ctx context.Context, ac CloudAccount, region, kind string) ([]Resource, error) {
	product, version := cloudProduct(kind)
	rows := []Resource{}
	for offset := 0; ; offset += 100 {
		body, e := t.call(ctx, ac, region, product, version, "DescribeInstances", map[string]any{"Offset": offset, "Limit": 100})
		if e != nil {
			return nil, e
		}
		items := array(body, "InstanceSet")
		if _, present := body["InstanceSet"]; !present {
			return nil, &CloudError{Code: "InvalidInstanceListResponse", Definite: false}
		}
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			r := Resource{AccountID: ac.ID, Provider: "tencent", Kind: kind, Region: region, InstanceID: str(m, "InstanceId"), Name: str(m, "InstanceName"), State: str(m, "InstanceState"), Spec: str(m, "InstanceType"), CPU: num(m, "CPU"), Memory: num(m, "Memory"), PublicIP: ips(m, "PublicIpAddresses"), PrivateIP: ips(m, "PrivateIpAddresses"), Tags: map[string]string{}, Metrics: map[string]Metric{}, SyncedAt: time.Now()}
			if kind == "lighthouse" {
				r.Spec = str(m, "BundleId")
				r.Disk = num(obj(m, "SystemDisk"), "DiskSize")
			} else {
				r.Disk = num(obj(m, "SystemDisk"), "DiskSize")
				for _, d := range array(m, "DataDisks") {
					if dm, ok := d.(map[string]any); ok {
						r.Disk += num(dm, "DiskSize")
					}
				}
			}
			if _, present := m["Tags"]; !present {
				r.TagsError = "云接口未返回标签字段"
			}
			for _, tag := range array(m, "Tags") {
				if tm, ok := tag.(map[string]any); ok {
					k, v := str(tm, "Key"), str(tm, "Value")
					if k == "" {
						k, v = str(tm, "TagKey"), str(tm, "TagValue")
					}
					if k != "" {
						r.Tags[k] = v
					}
				}
			}
			if r.InstanceID != "" {
				rows = append(rows, r)
			}
		}
		if len(items) < 100 {
			break
		}
	}
	return rows, nil
}
func OperationParameters(r Resource, action string) (string, map[string]any) {
	params := map[string]any{"InstanceIds": []string{r.InstanceID}}
	api := "RebootInstances"
	if action == "start" {
		api = "StartInstances"
	} else if action == "stop" || action == "force_stop" {
		api = "StopInstances"
	}
	// 两个产品都显式选择关机方式，特别是 Lighthouse 的默认值会自动强制关闭。
	if action != "start" {
		params["StopType"] = "SOFT"
		if strings.HasPrefix(action, "force_") {
			params["StopType"] = "HARD"
		}
	}
	return api, params
}
func (t *Tencent) Execute(ctx context.Context, ac CloudAccount, r Resource, action string) (string, error) {
	if !isServer(r.Kind) {
		return "", &CloudError{Code: "ReadOnlyResource", Definite: true}
	}
	service, version := cloudProduct(r.Kind)
	api, params := OperationParameters(r, action)
	body, e := t.call(ctx, ac, r.Region, service, version, api, params)
	if e != nil {
		return "", e
	}
	return str(body, "RequestId"), nil
}
func (t *Tencent) State(ctx context.Context, ac CloudAccount, r Resource) (CloudState, error) {
	service, version := cloudProduct(r.Kind)
	body, e := t.call(ctx, ac, r.Region, service, version, "DescribeInstances", map[string]any{"InstanceIds": []string{r.InstanceID}})
	if e != nil {
		return CloudState{}, e
	}
	items := array(body, "InstanceSet")
	if len(items) == 0 {
		return CloudState{}, &CloudError{Code: "ResourceNotFound", Definite: true}
	}
	m, _ := items[0].(map[string]any)
	return CloudState{str(m, "InstanceState"), str(m, "LatestOperationState"), str(m, "LatestOperationRequestId")}, nil
}
func (t *Tencent) Kinds(CloudAccount) []string {
	return []string{"cvm", "lighthouse"}
}

func (t *Tencent) MonitorBatch(ctx context.Context, ac CloudAccount, rows []Resource, enabled []string) map[string]map[string]Metric {
	out := map[string]map[string]Metric{}
	if len(rows) == 0 {
		return out
	}
	region, kind := rows[0].Region, rows[0].Kind
	namespace := "QCE/CVM"
	names := map[string]string{"cpu": "CPUUsage", "memory": "MemUsage", "network_in": "WanIntraffic", "network_out": "WanOuttraffic", "disk": "CvmDiskUsage"}
	if kind == "lighthouse" {
		namespace = "QCE/LIGHTHOUSE"
		names = map[string]string{"cpu": "CpuUsage", "memory": "MemUsage", "network_in": "LighthouseIntraffic", "network_out": "LighthouseOuttraffic", "disk": "DiskUsage"}
	}
	running := []string{}
	for _, r := range rows {
		out[r.InstanceID] = map[string]Metric{"disk:mounts": {Status: "unavailable", Reason: fmt.Sprintf("%s 云监控当前查询未提供挂载点明细", kind)}}
		valid := r.AccountID == ac.ID && r.Region == region && r.Kind == kind
		for _, key := range enabled {
			unit := "%"
			if strings.HasPrefix(key, "network_") {
				unit = "Mbps"
				if kind == "lighthouse" {
					unit = "MB/s"
				}
			}
			m := Metric{Unit: unit, Status: "unavailable", Reason: "云监控暂无数据，请检查指标可用条件"}
			if !valid {
				m.Status = "error"
				m.Reason = "监控请求混入其他账号、地域或资源类型"
			} else if r.State != "RUNNING" {
				m.Reason = "实例未运行，无当前监控数据"
			}
			out[r.InstanceID][key] = m
		}
		if !valid {
			return out
		}
		if r.State == "RUNNING" {
			running = append(running, r.InstanceID)
		}
	}
	// 腾讯云每次只允许同一地域、命名空间的一个指标，最多携带 50 个实例。
	end := time.Now().UTC()
	for start := 0; start < len(running); start += 50 {
		ids := running[start:min(start+50, len(running))]
		instances := make([]any, 0, len(ids))
		for _, id := range ids {
			instances = append(instances, map[string]any{"Dimensions": []any{map[string]any{"Name": "InstanceId", "Value": id}}})
		}
		requested := map[string]bool{}
		for _, id := range ids {
			requested[id] = true
		}
		for _, key := range enabled {
			body, err := t.call(ctx, ac, region, "monitor", "2018-07-24", "GetMonitorData", map[string]any{"Namespace": namespace, "MetricName": names[key], "Period": 60, "StartTime": end.Add(-5 * time.Minute).Format(time.RFC3339), "EndTime": end.Format(time.RFC3339), "Instances": instances})
			if err != nil {
				for _, id := range ids {
					m := out[id][key]
					m.Status = "error"
					m.Reason = "云监控请求失败：" + err.Error()
					out[id][key] = m
				}
				continue
			}
			// 响应可能乱序或缺少实例，必须用维度中的实例 ID 匹配，不能依赖数组下标。
			for _, item := range array(body, "DataPoints") {
				dp, ok := item.(map[string]any)
				if !ok {
					continue
				}
				id := ""
				for _, d := range array(dp, "Dimensions") {
					if dim, ok := d.(map[string]any); ok && str(dim, "Name") == "InstanceId" {
						id = str(dim, "Value")
					}
				}
				if !requested[id] {
					continue
				}
				values, times := array(dp, "Values"), array(dp, "Timestamps")
				if len(values) == 0 || len(values) != len(times) {
					continue
				}
				index := len(values) - 1
				value, ok := values[index].(float64)
				ts, okTime := times[index].(float64)
				if !ok || !okTime {
					continue
				}
				sampled := time.Unix(int64(ts), 0)
				m := out[id][key]
				m.Value = &value
				m.SampledAt = &sampled
				m.Status = "ok"
				m.Reason = ""
				if key == "disk" {
					m.Reason = "云端实例级空间使用率；未返回挂载点明细"
				}
				out[id][key] = m
			}
		}
	}
	return out
}
