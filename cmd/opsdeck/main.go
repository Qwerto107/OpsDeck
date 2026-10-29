package main

import (
	"context"
	"crypto/cipher"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"opsdeck/internal/app"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if e := run(); e != nil {
		slog.Error("startup failed", "error", e.Error())
		os.Exit(1)
	}
}
func run() error {
	command := "all"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command != "migrate" && command != "init" && command != "bootstrap" && command != "api" && command != "all" && command != "worker" {
		return errors.New("支持的命令：bootstrap、migrate、init、all、api、worker")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("请设置 DATABASE_URL")
	}
	if command == "migrate" {
		return migrateDatabase(dsn)
	}
	if command == "bootstrap" {
		if err := waitDatabase(ctx, dsn); err != nil {
			return err
		}
		if err := migrateDatabase(dsn); err != nil {
			return err
		}
	}
	db, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), TranslateError: true})
	if e != nil {
		return errors.New("数据库连接失败")
	}
	sqlDB, e := db.DB()
	if e != nil {
		return e
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(15)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	keyPath := os.Getenv("MASTER_KEY_FILE")
	if keyPath == "" {
		keyPath = "secrets/master.key"
	}
	var key cipher.AEAD
	if command == "bootstrap" {
		key, e = bootstrap(db.WithContext(ctx), keyPath)
	} else {
		key, e = app.OpenKey(db, keyPath, command == "init")
	}
	if e != nil {
		return e
	}
	if command == "init" {
		return initialize(db)
	}
	a := &app.App{DB: db, Key: key, Cloud: app.NewCloud(key)}
	if command == "worker" {
		a.RunWorker(ctx)
		return nil
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	server := &http.Server{Addr: addr, Handler: a.Router(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}
	var worker func(context.Context)
	if command == "all" || command == "bootstrap" {
		worker = a.RunWorker
	}
	return serve(ctx, server, worker)
}

func serve(parent context.Context, server *http.Server, worker func(context.Context)) error {
	// 先绑定端口，避免 HTTP 启动失败后仍领取后台任务。
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	workerDone := make(chan struct{})
	if worker != nil {
		go func() {
			defer close(workerDone)
			slog.Info("worker started")
			worker(ctx)
			slog.Info("worker stopped")
		}()
	} else {
		close(workerDone)
	}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}()
	slog.Info("api listening", "address", listener.Addr().String())
	err = server.Serve(listener)
	// 端口异常或收到退出信号时停止 Worker，等待任务收尾后再关闭数据库。
	cancel()
	<-workerDone
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func initialize(db *gorm.DB) error {
	username, password := initialCredentials()
	if err := initializeAdmin(db, username, password); err != nil {
		return err
	}
	logInitialCredentials(username, password)
	return nil
}

func initialCredentials() (string, string) {
	username := strings.TrimSpace(os.Getenv("INIT_USERNAME"))
	if username == "" {
		username = "admin"
	}
	return username, app.RandomToken()[:24]
}

func logInitialCredentials(username, password string) {
	// 仅首次初始化提交成功后输出临时密码；普通登录和后续启动不得输出密码。
	slog.Info("首次管理员已创建，请登录后修改临时密码并备份主密钥", "username", username, "temporary_password", password)
}

func initializeAdmin(db *gorm.DB, username, password string) error {
	hash, e := bcrypt.GenerateFromPassword([]byte(password), 12)
	if e != nil {
		return e
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if e := tx.Model(&app.User{}).Count(&count).Error; e != nil {
			return e
		}
		if count > 0 {
			return errors.New("用户已存在，禁止重复初始化管理员")
		}
		roles := []app.Role{{Name: "管理员", Administrator: true, Permissions: []string{}, Scopes: []app.Scope{}}, {Name: "只读人员", Permissions: []string{"resources.read", "monitor.read"}, Scopes: []app.Scope{}}, {Name: "运维人员", Permissions: []string{"resources.read", "monitor.read", "instances.start", "instances.stop", "instances.reboot"}, Scopes: []app.Scope{}}}
		for i := range roles {
			if e := tx.Create(&roles[i]).Error; e != nil {
				return e
			}
		}
		user := app.User{Username: username, DisplayName: username, PasswordHash: string(hash), Enabled: true, MustChangePassword: true, Preferences: map[string]any{}, Roles: []app.Role{roles[0]}}
		if e := tx.Create(&user).Error; e != nil {
			return e
		}
		return tx.Create(&app.Audit{UserID: &user.ID, Actor: username, Action: "system.initialize", Result: "success"}).Error
	})
}
