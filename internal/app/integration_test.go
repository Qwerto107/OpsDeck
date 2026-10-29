package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"opsdeck/migrations"
)

func integrationApp(t *testing.T) (*App, User) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("设置 TEST_DATABASE_URL 后运行 PostgreSQL 集成测试")
	}
	root, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	schema := "opsdeck_test_" + TokenHash(RandomToken())[:12]
	if e = root.Exec("CREATE SCHEMA " + schema).Error; e != nil {
		t.Fatal(e)
	}
	u, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, e := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		raw, _ := db.DB()
		_ = raw.Close()
		_ = root.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		raw, _ = root.DB()
		_ = raw.Close()
	})
	sql, e := migrations.Files.ReadFile("000001_initial.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Exec(string(sql)).Error; e != nil {
		t.Fatal(e)
	}
	sql, e = migrations.Files.ReadFile("000002_cloud_sites.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Exec(string(sql)).Error; e != nil {
		t.Fatal(e)
	}
	sql, e = migrations.Files.ReadFile("000003_database_resources.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Exec(string(sql)).Error; e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"000004_oauth_binding.up.sql", "000005_session_defaults.up.sql", "000006_monitor_refresh.up.sql"} {
		sql, e = migrations.Files.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		if e = db.Exec(string(sql)).Error; e != nil {
			t.Fatal(e)
		}
	}
	key, e := OpenKey(db, filepath.Join(t.TempDir(), "master.key"), true)
	if e != nil {
		t.Fatal(e)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-password-123"), bcrypt.MinCost)
	role := Role{Name: "test-admin", Administrator: true, Permissions: []string{}, Scopes: []Scope{}}
	if e = db.Create(&role).Error; e != nil {
		t.Fatal(e)
	}
	admin := User{Username: "test-admin", DisplayName: "测试管理员", PasswordHash: string(hash), Enabled: true, Roles: []Role{role}, Preferences: map[string]any{}}
	if e = db.Create(&admin).Error; e != nil {
		t.Fatal(e)
	}
	return &App{DB: db, Key: key, Origin: "http://localhost:8080", Cloud: &fakeCloud{state: CloudState{State: "RUNNING"}}}, admin
}
func sendRequest(a *App, method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", a.Origin)
	req.Header.Set("X-CSRF-Token", csrf)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.Router().ServeHTTP(w, req)
	return w
}
func loginTest(t *testing.T, a *App, username string) (*http.Cookie, string) {
	t.Helper()
	w := sendRequest(a, "POST", "/api/login", map[string]string{"username": username, "password": "test-password-123"}, nil, "")
	if w.Code != 200 {
		t.Fatalf("登录失败：%d %s", w.Code, w.Body.String())
	}
	var out struct {
		CSRF string `json:"csrf_token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Result().Cookies()[0], out.CSRF
}
func requireCode(t *testing.T, w *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if w.Code != expected {
		t.Fatalf("状态 %d，预期 %d：%s", w.Code, expected, w.Body.String())
	}
}
func TestPostgresAuthenticationAndAudit(t *testing.T) {
	a, admin := integrationApp(t)
	cookie, csrf := loginTest(t, a, admin.Username)
	requireCode(t, sendRequest(a, "GET", "/api/me", nil, cookie, ""), 200)
	requireCode(t, sendRequest(a, "PUT", "/api/preferences", map[string]any{}, cookie, ""), 403)
	requireCode(t, sendRequest(a, "PUT", "/api/preferences", map[string]any{"columns": []string{"state"}}, cookie, csrf), 204)
	requireCode(t, sendRequest(a, "POST", "/api/login", map[string]string{"username": admin.Username, "password": "wrong"}, nil, ""), 401)
	var count int64
	a.DB.Model(&Audit{}).Where("action = ? AND result = ?", "auth.login", "failed").Count(&count)
	if count != 1 {
		t.Fatal("登录失败没有审计")
	}
	settings, err := a.settings()
	if err != nil {
		t.Fatal(err)
	}
	a.DB.Model(&Session{}).Where("user_id = ?", admin.ID).Update("last_seen", time.Now().Add(-time.Duration(settings.IdleMinutes+1)*time.Minute))
	requireCode(t, sendRequest(a, "GET", "/api/me", nil, cookie, ""), 401)
	cookie, csrf = loginTest(t, a, admin.Username)
	requireCode(t, sendRequest(a, "POST", "/api/logout", nil, cookie, csrf), 204)
	requireCode(t, sendRequest(a, "GET", "/api/me", nil, cookie, ""), 401)
}
func TestPostgresScopeSortAndOperationDedup(t *testing.T) {
	a, admin := integrationApp(t)
	account := CloudAccount{Name: "测试账号", Provider: "tencent", SecretID: "encrypted", SecretKey: "encrypted", Regions: []string{"ap-beijing"}, Enabled: true}
	if e := a.DB.Create(&account).Error; e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"charlie", "alpha", "bravo"} {
		r := Resource{AccountID: account.ID, Provider: "tencent", Kind: "cvm", Region: "ap-beijing", InstanceID: "ins-" + name, Name: name, State: "RUNNING", Tags: map[string]string{"env": "prod"}, Metrics: map[string]Metric{}}
		if e := a.DB.Create(&r).Error; e != nil {
			t.Fatal(e)
		}
	}
	cookie, csrf := loginTest(t, a, admin.Username)
	path := "/api/resources?page_size=1&sort=" + url.QueryEscape(`[{"field":"name","desc":false}]`)
	w := sendRequest(a, "GET", path, nil, cookie, "")
	requireCode(t, w, 200)
	var page struct {
		Items []Resource
		Total int
	}
	if e := json.Unmarshal(w.Body.Bytes(), &page); e != nil {
		t.Fatal(e)
	}
	if page.Total != 3 || page.Items[0].Name != "alpha" {
		t.Fatal("分页前没有排序完整结果集")
	}
	id := page.Items[0].ID
	requireCode(t, sendRequest(a, "POST", "/api/resources/"+fmtID(id)+"/actions", map[string]string{"action": "force_stop", "confirm": "wrong"}, cookie, csrf), 400)
	requireCode(t, sendRequest(a, "POST", "/api/resources/"+fmtID(id)+"/actions", map[string]string{"action": "reboot"}, cookie, csrf), 202)
	requireCode(t, sendRequest(a, "POST", "/api/resources/"+fmtID(id)+"/actions", map[string]string{"action": "stop"}, cookie, csrf), 409)
	role := Role{Name: "readonly", Permissions: []string{"resources.read"}, Scopes: []Scope{{AccountID: account.ID, Kind: "lighthouse"}}}
	a.DB.Create(&role)
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-password-123"), 4)
	user := User{Username: "limited", DisplayName: "受限用户", PasswordHash: string(hash), Enabled: true, Preferences: map[string]any{}, Roles: []Role{role}}
	a.DB.Create(&user)
	limited, token := loginTest(t, a, user.Username)
	w = sendRequest(a, "GET", "/api/resources", nil, limited, "")
	requireCode(t, w, 200)
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if page.Total != 0 {
		t.Fatal("泄露了资源范围外的实例")
	}
	requireCode(t, sendRequest(a, "POST", "/api/resources/"+fmtID(id)+"/actions", map[string]string{"action": "stop"}, limited, token), 403)
	requireCode(t, sendRequest(a, "GET", "/api/resources/1%20OR%201=1", nil, cookie, ""), 400)
}
func fmtID(id uint64) string { b, _ := json.Marshal(id); return string(b) }

type fakeCloud struct {
	state        CloudState
	calls        int
	executeError error
}

func (f *fakeCloud) Regions(context.Context, CloudAccount, string) ([]string, error) {
	return []string{"ap-beijing"}, nil
}

func (f *fakeCloud) List(context.Context, CloudAccount, string, string) ([]Resource, error) {
	return nil, nil
}
func (f *fakeCloud) Execute(context.Context, CloudAccount, Resource, string) (string, error) {
	f.calls++
	return "req-test", f.executeError
}
func (f *fakeCloud) State(context.Context, CloudAccount, Resource) (CloudState, error) {
	return f.state, nil
}
func (f *fakeCloud) Kinds(CloudAccount) []string { return []string{"cvm", "lighthouse"} }
func (f *fakeCloud) MonitorBatch(context.Context, CloudAccount, []Resource, []string) map[string]map[string]Metric {
	return map[string]map[string]Metric{}
}
func TestPostgresWorkerNeverResubmitsUncertainOperation(t *testing.T) {
	a, u := integrationApp(t)
	f := &fakeCloud{state: CloudState{State: "RUNNING"}, executeError: &CloudError{Code: "ClientError.Timeout", Definite: false}}
	a.Cloud = f
	ac := CloudAccount{Name: "test", SecretID: "secret", SecretKey: "secret", Regions: []string{}, Enabled: true}
	a.DB.Create(&ac)
	r := Resource{AccountID: ac.ID, Provider: "tencent", Kind: "lighthouse", Region: "ap-beijing", InstanceID: "lhins-test", Name: "test", State: "RUNNING", Tags: map[string]string{}, Metrics: map[string]Metric{}}
	a.DB.Create(&r)
	audit := Audit{UserID: &u.ID, Actor: u.Username, Action: "instances.reboot", Result: "accepted"}
	a.DB.Create(&audit)
	task := Task{AccountID: &ac.ID, ResourceID: &r.ID, AuditID: &audit.ID, Action: "reboot", Status: "queued", BaselineState: "RUNNING", NextRun: time.Now()}
	if e := a.DB.Create(&task).Error; e != nil {
		t.Fatal(e)
	}
	a.process(context.Background(), task)
	a.DB.First(&task, task.ID)
	if task.Status != "unknown" || f.calls != 1 {
		t.Fatal("超时应进入待确认")
	}
	a.process(context.Background(), task)
	a.DB.First(&task, task.ID)
	if f.calls != 1 || task.Status != "unknown" {
		t.Fatal("待确认请求被重复提交或误判成功")
	}
	f.state = CloudState{State: "REBOOTING"}
	a.process(context.Background(), task)
	a.DB.First(&task, task.ID)
	f.state = CloudState{State: "RUNNING"}
	a.process(context.Background(), task)
	a.DB.First(&task, task.ID)
	if task.Status != "succeeded" {
		t.Fatal("观察到状态变迁后未确认成功")
	}
}
func TestPostgresMissingKeyAndRetentionConfirmation(t *testing.T) {
	a, u := integrationApp(t)
	if _, e := OpenKey(a.DB, filepath.Join(t.TempDir(), "missing.key"), true); e == nil {
		t.Fatal("已有数据时重新生成了密钥")
	}
	cookie, csrf := loginTest(t, a, u.Username)
	s, _ := a.settings()
	s.RetentionDays = 30
	requireCode(t, sendRequest(a, "PUT", "/api/settings", s, cookie, csrf), 400)
	var count int64
	a.DB.Model(&Audit{}).Where("action = ?", "settings.save").Count(&count)
	if count != 0 {
		t.Fatal("未确认的设置变更不应成功")
	}
	w := sendRequest(a, "GET", "/api/me", nil, cookie, "")
	if strings.Contains(w.Body.String(), "password_hash") {
		t.Fatal("响应泄露了密码哈希")
	}
}
