package app

import (
	"testing"
	"time"
)

func TestPostgresDeleteAccountUserAndRole(t *testing.T) {
	a, admin := integrationApp(t)
	cookie, csrf := loginTest(t, a, admin.Username)
	account := CloudAccount{Name: "delete-account", Provider: "tencent", SecretID: "encrypted", SecretKey: "encrypted", Regions: []string{}, Enabled: true}
	if err := a.DB.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	resource := Resource{AccountID: account.ID, Provider: "tencent", Kind: "cvm", Region: "ap-beijing", InstanceID: "ins-delete", Name: "test", State: "RUNNING", Tags: map[string]string{}, Metrics: map[string]Metric{}}
	if err := a.DB.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	role := Role{Name: "delete-role", Permissions: []string{"resources.read"}, Scopes: []Scope{{AccountID: account.ID}}}
	if err := a.DB.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	user := User{Username: "delete-user", DisplayName: "test", PasswordHash: "hash", Enabled: true, Preferences: map[string]any{}, Roles: []Role{role}}
	if err := a.DB.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	task := Task{AccountID: &account.ID, ResourceID: &resource.ID, Action: "reboot", Status: "queued", NextRun: time.Now()}
	if err := a.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	accountPath := "/api/accounts/" + fmtID(account.ID)
	requireCode(t, sendRequest(a, "DELETE", accountPath, nil, cookie, csrf), 409)
	if err := a.DB.Model(&task).Update("status", "succeeded").Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, sendRequest(a, "DELETE", accountPath, nil, cookie, csrf), 204)
	if err := a.DB.First(&task, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if task.AccountID != nil || task.ResourceID != nil {
		t.Fatal("已完成任务仍引用被删除的账号或资源")
	}
	if err := a.DB.First(&role, role.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(role.Scopes) != 0 {
		t.Fatal("角色仍保留已删除账号的资源范围")
	}
	var count int64
	a.DB.Model(&Resource{}).Where("account_id = ?", account.ID).Count(&count)
	if count != 0 {
		t.Fatal("资源快照未删除")
	}
	requireCode(t, sendRequest(a, "DELETE", "/api/users/"+fmtID(admin.ID), nil, cookie, csrf), 409)
	var adminRole Role
	if err := a.DB.Where("administrator = true").First(&adminRole).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, sendRequest(a, "DELETE", "/api/roles/"+fmtID(adminRole.ID), nil, cookie, csrf), 400)
	if err := a.DB.Create(&Audit{UserID: &user.ID, Actor: user.Username, Action: "auth.login", Result: "success"}).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, sendRequest(a, "DELETE", "/api/users/"+fmtID(user.ID), nil, cookie, ""), 403)
	requireCode(t, sendRequest(a, "DELETE", "/api/users/"+fmtID(user.ID), nil, cookie, csrf), 204)
	a.DB.Model(&Audit{}).Where("actor = ? AND user_id IS NULL", user.Username).Count(&count)
	if count != 1 {
		t.Fatal("用户删除后审计记录未保留")
	}
	requireCode(t, sendRequest(a, "DELETE", "/api/roles/"+fmtID(role.ID), nil, cookie, csrf), 204)
	a.DB.Model(&Audit{}).Where("action IN ? AND result = ?", []string{"accounts.delete", "users.delete", "roles.delete"}, "success").Count(&count)
	if count != 3 {
		t.Fatal("删除操作未完整记录审计")
	}
}
