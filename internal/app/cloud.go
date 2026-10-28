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
	Monitor(context.Context, CloudAccount, Resource, []string) map[string]Metric
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
func (t *Tencent) Monitor(ctx context.Context, ac CloudAccount, r Resource, enabled []string) map[string]Metric {
	out := map[string]Metric{}
	namespace := "QCE/CVM"
	names := map[string]string{"cpu": "CPUUsage", "memory": "MemUsage", "network_in": "WanIntraffic", "network_out": "WanOuttraffic", "disk": "CvmDiskUsage"}
	if r.Kind == "lighthouse" {
		namespace = "QCE/LIGHTHOUSE"
		names = map[string]string{"cpu": "CpuUsage", "memory": "MemUsage", "network_in": "LighthouseIntraffic", "network_out": "LighthouseOuttraffic", "disk": "DiskUsage"}
	}
	for _, key := range enabled {
		unit := "%"
		if strings.HasPrefix(key, "network_") {
			unit = "Mbps"
			if r.Kind == "lighthouse" {
				unit = "MB/s"
			}
		}
		metric := Metric{Unit: unit, Status: "unavailable", Reason: "云监控暂无数据，请检查指标可用条件"}
		if r.State != "RUNNING" {
			metric.Reason = "实例未运行，无当前监控数据"
			out[key] = metric
			continue
		}
		body, e := t.call(ctx, ac, r.Region, "monitor", "2018-07-24", "GetMonitorData", map[string]any{"Namespace": namespace, "MetricName": names[key], "Period": 60, "StartTime": time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339), "EndTime": time.Now().UTC().Format(time.RFC3339), "Instances": []any{map[string]any{"Dimensions": []any{map[string]any{"Name": "InstanceId", "Value": r.InstanceID}}}}})
		if e != nil {
			metric.Status = "error"
			metric.Reason = "云监控请求失败：" + e.Error()
			out[key] = metric
			continue
		}
		for _, dp := range array(body, "DataPoints") {
			m, ok := dp.(map[string]any)
			if !ok {
				continue
			}
			values, timestamps := array(m, "Values"), array(m, "Timestamps")
			if len(values) == 0 || len(values) != len(timestamps) {
				continue
			}
			i := len(values) - 1
			v, ok := values[i].(float64)
			ts, okTS := timestamps[i].(float64)
			if !ok || !okTS {
				continue
			}
			sampled := time.Unix(int64(ts), 0)
			metric.Value = &v
			metric.SampledAt = &sampled
			metric.Status = "ok"
			metric.Reason = ""
		}
		if key == "disk" && metric.Value != nil {
			metric.Reason = "云端实例级空间使用率；未返回挂载点明细"
		}
		out[key] = metric
		if e := ctx.Err(); e != nil {
			break
		}
	}
	// 实例级最大占用不能冒充某个挂载点；详情页明确保留明细不可用状态。
	out["disk:mounts"] = Metric{Status: "unavailable", Reason: fmt.Sprintf("%s 云监控当前查询未提供挂载点明细", r.Kind)}
	return out
}
