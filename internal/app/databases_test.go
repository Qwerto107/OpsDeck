package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTencentDatabaseMapping(t *testing.T) {
	for _, kind := range []string{"mysql", "postgresql"} {
		t.Run(kind, func(t *testing.T) {
			client, ac := monitorClient(t, func(params map[string]any) map[string]any {
				if kind == "mysql" {
					return map[string]any{"Items": []any{map[string]any{"InstanceId": "cdb-test", "InstanceName": "mysql", "Status": 1, "Cpu": 2, "Memory": 512, "Volume": 40, "EngineVersion": "8.0", "Vip": "10.0.0.1", "Vport": 3306, "WanDomain": "db.example.test", "WanPort": 13306, "TagList": []any{map[string]any{"TagKey": "env", "TagValue": "test"}}}}}
				}
				return map[string]any{"DBInstanceSet": []any{map[string]any{"DBInstanceId": "postgres-test", "DBInstanceName": "pg", "DBInstanceStatus": "running", "DBInstanceCpu": 4, "DBInstanceMemory": 8, "DBInstanceStorage": 100, "DBVersion": "16.4", "DBInstanceNetInfo": []any{map[string]any{"Address": "pg.example.test", "Port": 5432, "NetType": "private", "Status": "opened"}, map[string]any{"Address": "closed.example.test", "Port": 5432, "NetType": "public", "Status": "closed"}}, "TagList": []any{}}}}
			})
			ac.Provider = "tencent"
			rows, err := client.List(context.Background(), ac, "ap-guangzhou", kind)
			if err != nil || len(rows) != 1 {
				t.Fatalf("%v %v", rows, err)
			}
			r := rows[0]
			if r.Database == nil || r.Database.Version == "" || r.State != "RUNNING" {
				t.Fatalf("数据库解析错误: %+v", r)
			}
			if kind == "mysql" && (r.Database.MemoryGB != 0.5 || len(r.Database.Endpoints) != 2 || r.Tags["env"] != "test") {
				t.Fatal("MySQL 内存单位、地址或标签解析错误")
			}
			if kind == "postgresql" && (r.Database.MemoryGB != 8 || len(r.Database.Endpoints) != 1) {
				t.Fatal("PostgreSQL 内存单位或关闭地址解析错误")
			}
			if _, err = client.Execute(context.Background(), ac, r, "reboot"); err == nil {
				t.Fatal("数据库适配层不应接受服务器操作")
			}
		})
	}
}

func TestAlibabaRDSReadOnlyMapping(t *testing.T) {
	a, ac := alibabaFixture(t, func(r *http.Request) map[string]any {
		switch r.URL.Query().Get("Action") {
		case "DescribeDBInstances":
			return map[string]any{"Items": map[string]any{"DBInstance": []any{map[string]any{"DBInstanceId": "rm-test", "DBInstanceDescription": "database", "Engine": "MySQL"}}}}
		case "DescribeDBInstanceAttribute":
			return map[string]any{"Items": map[string]any{"DBInstanceAttribute": []any{map[string]any{"DBInstanceId": "rm-test", "DBInstanceCPU": "2", "DBInstanceMemory": 512, "DBInstanceStorage": 50, "EngineVersion": "8.0", "DBInstanceStatus": "Running", "DisasterRecoveryInfo": "secret-not-for-storage"}}}}
		case "DescribeDBInstanceNetInfo":
			return map[string]any{"DBInstanceNetInfos": map[string]any{"DBInstanceNetInfo": []any{map[string]any{"ConnectionString": "db.example.test", "Port": "3306", "IPType": "Private"}}}}
		case "DescribeTags":
			return map[string]any{"Items": map[string]any{"TagInfos": []any{map[string]any{"TagKey": "env", "TagValue": "test"}}}}
		}
		t.Fatalf("不应调用其他接口: %s", r.URL.Query().Get("Action"))
		return nil
	})
	rows, err := a.List(context.Background(), ac, "ap-southeast-1", "mysql")
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %v", rows, err)
	}
	r := rows[0]
	if r.Database == nil || r.Database.MemoryGB != 0.5 || r.Database.Endpoints[0].Port != 3306 || r.Tags["env"] != "test" {
		t.Fatalf("RDS 解析错误: %+v", r)
	}
	data, _ := json.Marshal(r)
	if string(data) == "" || strings.Contains(string(data), "secret-not-for-storage") {
		t.Fatal("数据库响应混入敏感配置")
	}
	if _, err = a.Execute(context.Background(), ac, r, "stop"); err == nil {
		t.Fatal("RDS 不能执行服务器操作")
	}
}

func TestPostgresDatabaseResourcesAreScopedAndReadOnly(t *testing.T) {
	a, admin := integrationApp(t)
	ac := CloudAccount{Name: "db-account", Enabled: true, SecretID: "encrypted", SecretKey: "encrypted", Regions: []string{}}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	r := databaseResource(ac, "ap-guangzhou", "mysql", "db-test", "数据库")
	r.State = "RUNNING"
	r.Database.Version = "8.0"
	r.Database.MemoryGB = 0.5
	r.Database.Endpoints = []DatabaseEndpoint{{Address: "db.example.test", Port: 3306, Network: "private"}}
	if err := a.DB.Create(&r).Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginTest(t, a, admin.Username)
	for _, action := range []string{"start", "stop", "reboot", "force_stop", "force_reboot"} {
		requireCode(t, sendRequest(a, "POST", "/api/resources/"+fmtID(r.ID)+"/actions", map[string]string{"action": action, "confirm": "确认"}, cookie, csrf), 400)
	}
	w := sendRequest(a, "GET", "/api/resources?category=database&q=db.example.test", nil, cookie, "")
	requireCode(t, w, 200)
	var page struct {
		Items []Resource
		Total int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items[0].AllowedActions) != 0 || page.Items[0].Database.MemoryGB != 0.5 {
		t.Fatal("数据库列表或快照字段错误")
	}
	w = sendRequest(a, "GET", "/api/resources?category=server", nil, cookie, "")
	requireCode(t, w, 200)
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if page.Total != 0 {
		t.Fatal("数据库混入服务器列表")
	}
	role := Role{Name: "server-only", Permissions: []string{"resources.read"}, Scopes: []Scope{{AccountID: ac.ID, Kind: "cvm"}}}
	u := User{Username: "db-limited", PasswordHash: admin.PasswordHash, Enabled: true, Preferences: map[string]any{}, Roles: []Role{role}}
	if err := a.DB.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	limited, _ := loginTest(t, a, u.Username)
	requireCode(t, sendRequest(a, "GET", "/api/resources/"+fmtID(r.ID), nil, limited, ""), 403)
	task := Task{AccountID: &ac.ID, ResourceID: &r.ID, Action: "reboot", Status: "queued", NextRun: time.Now()}
	if err := a.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	a.process(context.Background(), task)
	var got Task
	if err := a.DB.First(&got, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" || a.Cloud.(*fakeCloud).calls != 0 {
		t.Fatal("Worker 未拦截数据库操作任务")
	}
}
