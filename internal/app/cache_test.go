package app

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPostgresCacheReadsDoNotSyncOrUseFailedAttemptTime(t *testing.T) {
	a, u := integrationApp(t)
	ac := CloudAccount{Name: "cached", SecretID: "encrypted", SecretKey: "encrypted", Regions: []string{}, Enabled: true}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	for _, task := range []Task{
		{AccountID: &ac.ID, Action: "sync", Status: "succeeded", UpdatedAt: stamp, NextRun: stamp},
		{AccountID: &ac.ID, Action: "sync", Status: "failed", UpdatedAt: stamp.Add(time.Minute), NextRun: stamp},
	} {
		if err := a.DB.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	cookie, _ := loginTest(t, a, u.Username)
	for i := 0; i < 2; i++ {
		response := sendRequest(a, "GET", "/api/resources", nil, cookie, "")
		requireCode(t, response, 200)
		var data struct {
			Cache resourceCache `json:"cache"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if data.Cache.TTLSeconds != 600 || data.Cache.UpdatedAt == nil || !data.Cache.UpdatedAt.Equal(stamp) {
			t.Fatalf("缓存时间使用了失败尝试或有效期错误: %#v", data.Cache)
		}
	}
	var count int64
	a.DB.Model(&Task{}).Count(&count)
	if count != 2 {
		t.Fatal("页面读取产生了额外同步任务")
	}
}

func TestPostgresMonitorAutoRefreshCache(t *testing.T) {
	a, admin := integrationApp(t)
	settings, err := a.settings()
	if err != nil || !settings.AutoRefresh {
		t.Fatal("自动刷新默认值应为开启")
	}
	ac := CloudAccount{Name: "auto-refresh", Enabled: true, Regions: []string{}}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	rows := []Resource{
		{AccountID: ac.ID, Kind: "cvm", InstanceID: "auto-first", State: "RUNNING", MonitorRefreshedAt: &stamp},
		{AccountID: ac.ID, Kind: "mysql", InstanceID: "auto-database", State: "RUNNING"},
		{AccountID: ac.ID, Kind: "cvm", InstanceID: "auto-stopped", State: "STOPPED"},
	}
	for i := range rows {
		rows[i].Tags, rows[i].Metrics = map[string]string{}, map[string]Metric{}
		if err := a.DB.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	cookie, csrf := loginTest(t, a, admin.Username)
	settings.AutoRefresh = true
	requireCode(t, sendRequest(a, "PUT", "/api/settings", settings, cookie, csrf), 200)
	for _, tc := range []struct {
		query   string
		enabled bool
		stamp   *time.Time
	}{
		{"?category=server&state=RUNNING", true, &stamp},
		{"?category=database", true, nil},
		{"?state=STOPPED", false, nil},
		{"?q=not-existing", false, nil},
	} {
		response := sendRequest(a, "GET", "/api/resources"+tc.query, nil, cookie, "")
		requireCode(t, response, 200)
		var data struct{ Cache resourceCache }
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if data.Cache.AutoRefresh != tc.enabled || (data.Cache.MonitorUpdatedAt == nil) != (tc.stamp == nil) ||
			(tc.stamp != nil && !data.Cache.MonitorUpdatedAt.Equal(*tc.stamp)) {
			t.Fatalf("自动刷新范围或缓存时间错误: %s, %+v", tc.query, data.Cache)
		}
	}
	var count int64
	a.DB.Model(&Task{}).Count(&count)
	if count != 0 {
		t.Fatal("只读查询创建了刷新任务")
	}
	settings.AutoRefresh = false
	requireCode(t, sendRequest(a, "PUT", "/api/settings", settings, cookie, csrf), 200)
	stored, err := a.settings()
	if err != nil || stored.AutoRefresh {
		t.Fatal("关闭自动刷新未保存")
	}
}
