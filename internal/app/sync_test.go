package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestPostgresWorkerDoesNotScheduleResourceSync(t *testing.T) {
	a, _ := integrationApp(t)
	ac := CloudAccount{Name: "idle-account", SecretID: "encrypted", SecretKey: "encrypted", Regions: []string{}, Enabled: true}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	a.RunWorker(ctx)
	var count int64
	if err := a.DB.Model(&Task{}).Where("action = 'sync'").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("没有页面请求时 Worker 自行创建了同步任务")
	}
}

func TestRefreshPermissionDoesNotCrossRoles(t *testing.T) {
	u := User{Roles: []Role{
		{Permissions: []string{"resources.read"}, Scopes: []Scope{{AccountID: 1}}},
		{Permissions: []string{"monitor.read"}, Scopes: []Scope{{AccountID: 2}}},
	}}
	if !canRefreshAccount(u, 1) || canRefreshAccount(u, 2) {
		t.Fatal("刷新权限跨角色合并了账号范围")
	}
}

func TestPostgresPageRefreshDedupAndScope(t *testing.T) {
	a, u := integrationApp(t)
	ac := CloudAccount{Name: "refresh", SecretID: "encrypted", SecretKey: "encrypted", Regions: []string{}, Enabled: true}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginTest(t, a, u.Username)
	requireCode(t, sendRequest(a, "POST", "/api/resources/sync", map[string]any{}, cookie, ""), 403)
	var first struct {
		Tasks []syncProgress `json:"tasks"`
	}
	for i := 0; i < 2; i++ {
		w := sendRequest(a, "POST", "/api/resources/sync", map[string]any{}, cookie, csrf)
		requireCode(t, w, 202)
		if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
			t.Fatal(err)
		}
		if len(first.Tasks) != 1 {
			t.Fatal("未返回同步进度")
		}
	}
	var count int64
	a.DB.Model(&Task{}).Where("action = 'sync'").Count(&count)
	if count != 1 {
		t.Fatal("重复页面请求没有合并同步任务")
	}
	id := fmtID(first.Tasks[0].ID)
	requireCode(t, sendRequest(a, "GET", "/api/sync-tasks?ids="+id, nil, cookie, ""), 200)
	// 移除用户资源权限后，不能继续查询已知任务或触发新同步。
	if err := a.DB.Model(&u).Association("Roles").Clear(); err != nil {
		t.Fatal(err)
	}
	requireCode(t, sendRequest(a, "GET", "/api/sync-tasks?ids="+id, nil, cookie, ""), 403)
	requireCode(t, sendRequest(a, "POST", "/api/resources/sync", map[string]any{"account_id": ac.ID}, cookie, csrf), 403)
}
