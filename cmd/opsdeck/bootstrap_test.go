package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"opsdeck/internal/app"
)

func TestBootstrapInitializationAndKeyProtection(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("设置 TEST_DATABASE_URL 后运行 PostgreSQL 初始化测试")
	}
	root, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	rawRoot, _ := root.DB()
	defer rawRoot.Close()
	schema := "opsdeck_test_" + app.TokenHash(app.RandomToken())[:12]
	if err := root.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	defer root.Exec("DROP SCHEMA " + schema + " CASCADE")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	if err := migrateDatabase(u.String()); err != nil {
		t.Fatal(err)
	}
	if err := migrateDatabase(u.String()); err != nil {
		t.Fatal("重复迁移失败：", err)
	}
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	defer raw.Close()
	keyPath := filepath.Join(t.TempDir(), "master.key")
	t.Setenv("INIT_USERNAME", "")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := bootstrap(db, keyPath)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("并发初始化失败：", err)
		}
	}
	var initialization struct {
		Username string `json:"username"`
		Password string `json:"temporary_password"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &initialization); err != nil {
		t.Fatal("首次密码没有且仅有一次日志输出")
	}
	var user app.User
	if err := db.First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.Username != "admin" || initialization.Username != "admin" || !app.ValidPassword(initialization.Password) || !user.MustChangePassword || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(initialization.Password)) != nil {
		t.Fatal("初始管理员账号或密码不正确")
	}
	for table, want := range map[string]int64{"users": 1, "roles": 3, "key_checks": 1, "audits": 1} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != want {
			t.Fatalf("%s 记录数：%d，期望：%d，错误：%v", table, count, want, err)
		}
	}
	key, err := os.ReadFile(keyPath)
	if err != nil || len(key) != 32 {
		t.Fatal("主密钥未正确生成")
	}
	// 重启不得重置账号、密钥，或再次输出临时密码。
	initialLog := logs.String()
	t.Setenv("INIT_USERNAME", "another-admin")
	if _, err := bootstrap(db, keyPath); err != nil {
		t.Fatal("已有账号重复启动失败：", err)
	}
	if logs.String() != initialLog {
		t.Fatal("重复启动再次输出了临时密码")
	}
	var unchanged app.User
	if err := db.First(&unchanged, user.ID).Error; err != nil || unchanged.PasswordHash != user.PasswordHash {
		t.Fatal("重复启动修改了已有密码")
	}
	after, _ := os.ReadFile(keyPath)
	if !bytes.Equal(key, after) {
		t.Fatal("重复启动修改了主密钥")
	}
	missing := filepath.Join(t.TempDir(), "missing.key")
	if _, err := bootstrap(db, missing); err == nil {
		t.Fatal("已有账号缺少原密钥时仍启动成功")
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("已有账号时不应创建替代密钥")
	}
	wrong := filepath.Join(t.TempDir(), "wrong.key")
	if err := os.WriteFile(wrong, bytes.Repeat([]byte{1}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap(db, wrong); err == nil {
		t.Fatal("密钥不匹配时仍启动成功")
	}
}
