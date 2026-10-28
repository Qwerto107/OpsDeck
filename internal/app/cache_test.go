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
		if data.Cache.TTLSeconds != 180 || data.Cache.UpdatedAt == nil || !data.Cache.UpdatedAt.Equal(stamp) {
			t.Fatalf("缓存时间使用了失败尝试或有效期错误: %#v", data.Cache)
		}
	}
	var count int64
	a.DB.Model(&Task{}).Count(&count)
	if count != 2 {
		t.Fatal("页面读取产生了额外同步任务")
	}
}
