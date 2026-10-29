package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPostgresAuditSnapshotPreservesNamesAndInstanceResults(t *testing.T) {
	a, admin := integrationApp(t)
	ac := CloudAccount{Name: "生产账号", Enabled: true, SecretID: "encrypted", SecretKey: "encrypted", Regions: []string{}}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	resources := []Resource{}
	for _, id := range []string{"ins-one", "ins-two"} {
		r := Resource{AccountID: ac.ID, Name: "原名称-" + id, InstanceID: id, Kind: "cvm", Region: "ap-test", State: "RUNNING", Tags: map[string]string{}, Metrics: map[string]Metric{}}
		if err := a.DB.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
		resources = append(resources, r)
	}
	cookie, csrf := loginTest(t, a, admin.Username)
	w := sendRequest(a, "POST", "/api/resources/sync", map[string]any{"resource_ids": []uint64{resources[0].ID, resources[1].ID}}, cookie, csrf)
	requireCode(t, w, 202)
	var response struct{ Tasks []syncProgress }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var task Task
	a.DB.First(&task, response.Tasks[0].ID)
	a.Cloud = &refreshTestCloud{rows: resources, failed: "ins-two"}
	a.process(context.Background(), task)
	if err := a.DB.Model(&ac).Update("name", "改名后账号").Error; err != nil {
		t.Fatal(err)
	}
	if err := a.DB.Model(&resources[0]).Update("name", "改名后实例").Error; err != nil {
		t.Fatal(err)
	}
	if err := a.DB.Delete(&Resource{}, []uint64{resources[0].ID, resources[1].ID}).Error; err != nil {
		t.Fatal(err)
	}
	w = sendRequest(a, "GET", "/api/audits?action=resources.monitor&q="+"原名称", nil, cookie, "")
	requireCode(t, w, 200)
	var page struct {
		Items []Audit
		Total int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Total != 1 {
		t.Fatalf("审计名称搜索错误: %v", err)
	}
	row := page.Items[0]
	if row.TargetSource != "snapshot" || row.TargetLabel != "2 台实例" || len(row.TargetDetails) != 2 || row.TargetDetails[0].Name != "原名称-ins-one" || row.TargetDetails[0].AccountName != "生产账号" {
		t.Fatal("名称快照受资源改名或删除影响")
	}
	if row.Task == nil || len(row.Task.Results) != 2 || row.DisplayResult != "failed" || !strings.Contains(row.DisplayMessage, "成功 1 台") || !strings.Contains(row.DisplayMessage, "失败 1 台") {
		t.Fatal("未返回逐实例执行结果")
	}
	if !strings.HasPrefix(row.Target, "[") || row.RequestID == "" {
		t.Fatal("丢失原始标识")
	}
}

func TestPostgresLegacyAuditUsesTaskNamesWithoutRewritingHistory(t *testing.T) {
	a, admin := integrationApp(t)
	ac := CloudAccount{Name: "旧日志账号", Enabled: true, SecretID: "encrypted", SecretKey: "encrypted", Regions: []string{}}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	r := Resource{AccountID: ac.ID, Name: "当前名称", InstanceID: "ins-history", Kind: "cvm", Region: "ap-test", State: "RUNNING", Tags: map[string]string{}, Metrics: map[string]Metric{}}
	if err := a.DB.Create(&r).Error; err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal([]uint64{r.ID})
	legacy := Audit{Actor: admin.Username, Action: "resources.monitor", Target: string(encoded), Result: "success"}
	if err := a.DB.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	task := Task{AuditID: &legacy.ID, AccountID: &ac.ID, Action: "monitor", Status: "succeeded", ResourceIDs: []uint64{r.ID}, RefreshResults: []RefreshResult{{ResourceID: r.ID, Name: "操作时名称", InstanceID: r.InstanceID, Status: "succeeded"}}, NextRun: time.Now()}
	if err := a.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	submission := Audit{Actor: admin.Username, Action: "accounts.sync", Target: fmtID(ac.ID), Result: "success"}
	if err := a.DB.Create(&submission).Error; err != nil {
		t.Fatal(err)
	}
	cookie, _ := loginTest(t, a, admin.Username)
	w := sendRequest(a, "GET", "/api/audits?q=操作时名称&result=succeeded", nil, cookie, "")
	requireCode(t, w, 200)
	var page struct {
		Items []Audit
		Total int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Total != 1 {
		t.Fatalf("历史任务名称或执行结果过滤错误: %v", err)
	}
	if page.Items[0].TargetLabel != "操作时名称" || page.Items[0].TargetSource != "task" {
		t.Fatal("历史日志未优先采用任务名称")
	}
	var stored Audit
	a.DB.First(&stored, legacy.ID)
	if stored.TargetLabel != "" || stored.Target != string(encoded) || stored.Result != "success" {
		t.Fatal("读取历史记录改写了原审计数据")
	}
	w = sendRequest(a, "GET", "/api/audits?action=accounts.sync&result=accepted", nil, cookie, "")
	requireCode(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Total != 1 || page.Items[0].TargetLabel != ac.Name || page.Items[0].DisplayResult != "accepted" {
		t.Fatal("提交同步被误报为执行完成")
	}
	a.DB.Delete(&task)
	a.DB.Delete(&r)
	w = sendRequest(a, "GET", "/api/audits?action=resources.monitor", nil, cookie, "")
	requireCode(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || !strings.Contains(page.Items[0].TargetLabel, "无法解析") {
		t.Fatal("已删除历史目标没有明确标注")
	}
	if err := a.DB.Model(&admin).Association("Roles").Clear(); err != nil {
		t.Fatal(err)
	}
	requireCode(t, sendRequest(a, "GET", "/api/audits", nil, cookie, ""), 403)
}

func TestPostgresOperationAuditKeepsOriginalRequestID(t *testing.T) {
	a, admin := integrationApp(t)
	ac := CloudAccount{Name: "操作账号", Enabled: true, SecretID: "encrypted", SecretKey: "encrypted", Regions: []string{}}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	r := Resource{AccountID: ac.ID, Name: "应用服务器", InstanceID: "ins-operation", Kind: "cvm", Region: "ap-test", State: "RUNNING", Tags: map[string]string{}, Metrics: map[string]Metric{}}
	if err := a.DB.Create(&r).Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginTest(t, a, admin.Username)
	w := sendRequest(a, "POST", "/api/resources/"+fmtID(r.ID)+"/actions", map[string]string{"action": "reboot"}, cookie, csrf)
	requireCode(t, w, 202)
	var task Task
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	var before Audit
	a.DB.First(&before, *task.AuditID)
	a.Cloud = &fakeCloud{state: CloudState{State: "RUNNING"}}
	a.process(context.Background(), task)
	var after Audit
	a.DB.First(&after, before.ID)
	if before.RequestID == "" || after.RequestID != before.RequestID || after.TargetLabel != r.Name {
		t.Fatal("云端结果覆盖了原始请求标识或目标快照")
	}
}
