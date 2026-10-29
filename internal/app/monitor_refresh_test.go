package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

type refreshTestCloud struct {
	fakeCloud
	rows       []Resource
	failed     string
	mu         sync.Mutex
	requested  []string
	discovered bool
}

func (f *refreshTestCloud) Regions(context.Context, CloudAccount, string) ([]string, error) {
	f.discovered = true
	return nil, nil
}
func (f *refreshTestCloud) List(_ context.Context, ac CloudAccount, region, kind string) ([]Resource, error) {
	rows := []Resource{}
	for _, r := range f.rows {
		if r.AccountID == ac.ID && r.Region == region && r.Kind == kind {
			rows = append(rows, r)
		}
	}
	return rows, nil
}
func (f *refreshTestCloud) MonitorBatch(_ context.Context, _ CloudAccount, rows []Resource, enabled []string) map[string]map[string]Metric {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]map[string]Metric{}
	for _, r := range rows {
		f.requested = append(f.requested, r.InstanceID)
		values := map[string]Metric{}
		for _, key := range enabled {
			v, sampled := 42.0, time.Now().UTC()
			m := Metric{Value: &v, SampledAt: &sampled, Unit: "%", Status: "ok"}
			if r.InstanceID == f.failed {
				m = Metric{Status: "error", Reason: "云监控请求失败：UnauthorizedOperation"}
			}
			values[key] = m
		}
		out[r.InstanceID] = values
	}
	return out
}

func TestPostgresSelectedMonitorRefreshPreservesFailuresAndRetryScope(t *testing.T) {
	a, admin := integrationApp(t)
	ac := CloudAccount{Name: "monitor", Enabled: true, Regions: []string{}, SecretID: "encrypted", SecretKey: "encrypted"}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	cloud := &refreshTestCloud{failed: "ins-2"}
	a.Cloud = cloud
	oldValue, oldSample := 7.0, time.Now().Add(-12*time.Minute).UTC()
	for _, id := range []string{"ins-1", "ins-2", "ins-unselected"} {
		r := Resource{AccountID: ac.ID, Kind: "cvm", Region: "ap-test", InstanceID: id, Name: id, State: "RUNNING", Tags: map[string]string{}, Metrics: map[string]Metric{"cpu": {Value: &oldValue, SampledAt: &oldSample, Unit: "%", Status: "ok"}}}
		if err := a.DB.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
		cloud.rows = append(cloud.rows, r)
	}
	cookie, csrf := loginTest(t, a, admin.Username)
	ids := []uint64{cloud.rows[0].ID, cloud.rows[1].ID}
	start := func(ids []uint64) Task {
		t.Helper()
		w := sendRequest(a, "POST", "/api/resources/sync", map[string]any{"resource_ids": ids}, cookie, csrf)
		requireCode(t, w, 202)
		var data struct{ Tasks []syncProgress }
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil || len(data.Tasks) != 1 {
			t.Fatalf("任务创建失败: %v", err)
		}
		var task Task
		if err := a.DB.First(&task, data.Tasks[0].ID).Error; err != nil {
			t.Fatal(err)
		}
		return task
	}
	task := start(ids)
	if duplicate := start(ids); duplicate.ID != task.ID {
		t.Fatal("重复刷新没有合并")
	}
	a.process(context.Background(), task)
	if err := a.DB.First(&task, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != "failed" || len(task.RefreshResults) != 2 || task.RefreshResults[0].Status != "succeeded" || task.RefreshResults[1].Status != "failed" {
		t.Fatalf("逐实例结果错误: %+v", task.RefreshResults)
	}
	var good, bad, unselected Resource
	a.DB.First(&good, ids[0])
	a.DB.First(&bad, ids[1])
	a.DB.First(&unselected, cloud.rows[2].ID)
	if good.MonitorRefreshedAt == nil || bad.MonitorRefreshedAt != nil || unselected.MonitorRefreshedAt != nil {
		t.Fatal("成功获取时间错误，或失败项、未选项的缓存有效期被延长")
	}
	if *good.Metrics["cpu"].Value != 42 || *bad.Metrics["cpu"].Value != oldValue || !bad.Metrics["cpu"].SampledAt.Equal(oldSample) || *unselected.Metrics["cpu"].Value != oldValue {
		t.Fatal("成功项未更新，或失败项、未选项的旧数据被覆盖")
	}
	if cloud.discovered || len(cloud.requested) != 2 {
		t.Fatal("刷新当前页触发了地域发现或额外实例查询")
	}
	w := sendRequest(a, "GET", "/api/sync-tasks?ids="+fmtID(task.ID), nil, cookie, "")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), "UnauthorizedOperation") {
		t.Fatal("失败原因未返回")
	}
	cloud.failed, cloud.requested = "", nil
	retry := start([]uint64{ids[1]})
	a.process(context.Background(), retry)
	if len(cloud.requested) != 1 || cloud.requested[0] != "ins-2" {
		t.Fatal("重试刷新了非失败项")
	}
	var settings Settings
	a.DB.First(&settings, 1)
	if settings.StaleMinutes != 10 {
		t.Fatal("过期提示默认值错误")
	}
	settings.StaleMinutes = 20
	requireCode(t, sendRequest(a, "PUT", "/api/settings", settings, cookie, csrf), 200)
	w = sendRequest(a, "GET", "/api/resources?category=server", nil, cookie, "")
	requireCode(t, w, 200)
	var page struct{ Cache resourceCache }
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Cache.StaleAfterSeconds != 1200 || page.Cache.TTLSeconds != 1200 || page.Cache.SampledAt == nil || !page.Cache.SampledAt.Equal(oldSample) {
		t.Fatal("采样时间或自定义过期时间错误")
	}
}

func TestSelectedMonitorRefreshPermissionDoesNotCrossRoles(t *testing.T) {
	r := Resource{AccountID: 1, Kind: "lighthouse", Region: "ap-test", InstanceID: "ins-1"}
	u := User{Roles: []Role{
		{Permissions: []string{"resources.read"}, Scopes: []Scope{{AccountID: 1}}},
		{Permissions: []string{"monitor.read"}, Scopes: []Scope{{AccountID: 2}}},
	}}
	if canRefreshResource(u, r) {
		t.Fatal("监控刷新跨角色拼接了授权")
	}
	u.Roles[0].Permissions = append(u.Roles[0].Permissions, "monitor.read")
	if !canRefreshResource(u, r) {
		t.Fatal("同一角色的有效权限未生效")
	}
}

func TestPostgresSelectedMonitorRefreshRechecksAuthorization(t *testing.T) {
	a, admin := integrationApp(t)
	ac := CloudAccount{Name: "scope", Enabled: true, Regions: []string{}, SecretID: "encrypted", SecretKey: "encrypted"}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	r := Resource{AccountID: ac.ID, Kind: "cvm", Region: "ap-test", InstanceID: "ins-1", State: "RUNNING", Tags: map[string]string{}, Metrics: map[string]Metric{}}
	if err := a.DB.Create(&r).Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginTest(t, a, admin.Username)
	requireCode(t, sendRequest(a, "POST", "/api/resources/sync", map[string]any{"resource_ids": []uint64{}}, cookie, csrf), 400)
	w := sendRequest(a, "POST", "/api/resources/sync", map[string]any{"resource_ids": []uint64{r.ID}}, cookie, csrf)
	requireCode(t, w, 202)
	var data struct{ Tasks []syncProgress }
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if err := a.DB.Model(&admin).Association("Roles").Clear(); err != nil {
		t.Fatal(err)
	}
	requireCode(t, sendRequest(a, "GET", "/api/sync-tasks?ids="+fmtID(data.Tasks[0].ID), nil, cookie, ""), 403)
	var task Task
	a.DB.First(&task, data.Tasks[0].ID)
	cloud := &refreshTestCloud{rows: []Resource{r}}
	a.Cloud = cloud
	a.process(context.Background(), task)
	if len(cloud.requested) != 0 {
		t.Fatal("授权失效后仍调用云监控")
	}
	a.DB.First(&task, task.ID)
	if task.Status != "failed" {
		t.Fatal("授权失效任务未失败")
	}
}
