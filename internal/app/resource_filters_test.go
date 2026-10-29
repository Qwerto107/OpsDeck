package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"
)

func TestPostgresResourceMultiFiltersAndMetricPagination(t *testing.T) {
	a, admin := integrationApp(t)
	accounts := []CloudAccount{{Name: "first", Enabled: true}, {Name: "second", Enabled: true}, {Name: "unselected", Enabled: true}}
	for i := range accounts {
		accounts[i].Regions = []string{}
	}
	if err := a.DB.Create(&accounts).Error; err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	for i := range 2 {
		if err := a.DB.Create(&Task{AccountID: &accounts[i].ID, Action: "sync", Status: "succeeded", UpdatedAt: stamp, NextRun: stamp}).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows := []Resource{
		{AccountID: accounts[0].ID, Kind: "cvm", Region: "ap-a", State: "RUNNING", Name: "alpha"},
		{AccountID: accounts[1].ID, Kind: "ecs", Region: "ap-b", State: "STOPPED", Name: "beta"},
		{AccountID: accounts[0].ID, Kind: "lighthouse", Region: "ap-b", State: "RUNNING"},
		{AccountID: accounts[0].ID, Kind: "cvm", Region: "ap-c", State: "RUNNING"},
		{AccountID: accounts[1].ID, Kind: "ecs", Region: "ap-b", State: "CREATING"},
		{AccountID: accounts[2].ID, Kind: "cvm", Region: "ap-a", State: "RUNNING"},
		{AccountID: accounts[0].ID, Kind: "mysql", Region: "ap-a", State: "RUNNING"},
	}
	for i := range rows {
		value := float64(9 + i*71)
		rows[i].InstanceID = fmt.Sprintf("filter-%d", i)
		rows[i].CPU = 128 - i
		rows[i].Metrics = map[string]Metric{"cpu": {Value: &value, Status: "ok"}}
		rows[i].Tags = map[string]string{}
		if err := a.DB.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	cookie, _ := loginTest(t, a, admin.Username)
	filter := fmt.Sprintf("category=server&account_id=%d,%d&kind=cvm,ecs&region=ap-a&region=ap-b&state=RUNNING,STOPPED", accounts[0].ID, accounts[1].ID)
	sort := url.QueryEscape(`[{"field":"cpu","desc":true}]`)
	var result struct {
		Items []Resource
		Total int
		Cache resourceCache
	}
	for page, expected := range []string{"beta", "alpha"} {
		response := sendRequest(a, "GET", fmt.Sprintf("/api/resources?%s&sort=%s&page_size=1&page=%d", filter, sort, page+1), nil, cookie, "")
		requireCode(t, response, 200)
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Total != 2 || len(result.Items) != 1 || result.Items[0].Name != expected {
			t.Fatal("多选组合或按监控使用率排序后分页错误")
		}
		if result.Cache.UpdatedAt == nil || !result.Cache.UpdatedAt.Equal(stamp) {
			t.Fatal("多账号筛选的缓存时间受未选择账号影响")
		}
	}
	role := Role{Name: "filtered-reader", Permissions: []string{"resources.read"}, Scopes: []Scope{{AccountID: accounts[0].ID, Kind: "cvm", Region: "ap-a"}}}
	limited := User{Username: "filtered-reader", PasswordHash: admin.PasswordHash, Enabled: true, Roles: []Role{role}, Preferences: map[string]any{}}
	if err := a.DB.Create(&limited).Error; err != nil {
		t.Fatal(err)
	}
	limitedCookie, _ := loginTest(t, a, limited.Username)
	response := sendRequest(a, "GET", "/api/resources?"+filter+"&sort="+sort, nil, limitedCookie, "")
	requireCode(t, response, 200)
	result.Items = nil
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Total != 1 || len(result.Items) != 1 || result.Items[0].Name != "alpha" || len(result.Items[0].Metrics) != 0 {
		t.Fatal("多选或监控排序泄露了授权范围外的资源和指标")
	}
	response = sendRequest(a, "GET", fmt.Sprintf("/api/resources?account_id=%d&kind=cvm&region=ap-a&state=RUNNING", accounts[0].ID), nil, cookie, "")
	requireCode(t, response, 200)
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Total != 1 || result.Items[0].Name != "alpha" {
		t.Fatal("旧单选参数不再兼容")
	}
}
