package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"opsdeck/internal/app"
	"opsdeck/migrations"
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
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("请设置 DATABASE_URL")
	}
	if command == "migrate" {
		source, e := iofs.New(migrations.Files, ".")
		if e != nil {
			return e
		}
		m, e := migrate.NewWithSourceInstance("iofs", source, dsn)
		if e != nil {
			return errors.New("数据库迁移连接失败")
		}
		defer m.Close()
		e = m.Up()
		if errors.Is(e, migrate.ErrNoChange) {
			return nil
		}
		return e
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
	key, e := app.OpenKey(db, keyPath, command == "init")
	if e != nil {
		return e
	}
	if command == "init" {
		return initialize(db)
	}
	origin := strings.TrimSuffix(os.Getenv("APP_URL"), "/")
	if origin == "" {
		origin = "http://localhost:5173"
	}
	a := &app.App{DB: db, Key: key, Origin: origin, Secure: !strings.HasPrefix(origin, "http://localhost:") && !strings.HasPrefix(origin, "http://127.0.0.1:"), Cloud: &app.Tencent{Key: key}}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if command == "worker" {
		a.RunWorker(ctx)
		return nil
	}
	if command != "api" && command != "all" {
		return errors.New("支持的命令：migrate、init、all、api、worker")
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	server := &http.Server{Addr: addr, Handler: a.Router(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}
	var worker func(context.Context)
	if command == "all" {
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
	username := os.Getenv("INIT_USERNAME")
	password := os.Getenv("INIT_PASSWORD")
	if path := os.Getenv("INIT_PASSWORD_FILE"); path != "" {
		b, e := os.ReadFile(path)
		if e != nil {
			return errors.New("无法读取初始密码文件")
		}
		password = strings.TrimRight(string(b), "\r\n")
	}
	if username == "" || !app.ValidPassword(password) {
		return errors.New("首次初始化需设置 INIT_USERNAME 及 12 至 72 字节的 INIT_PASSWORD 或 INIT_PASSWORD_FILE")
	}
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
		user := app.User{Username: username, DisplayName: username, PasswordHash: string(hash), Enabled: true, MustChangePassword: false, Preferences: map[string]any{}, Roles: []app.Role{roles[0]}}
		if e := tx.Create(&user).Error; e != nil {
			return e
		}
		fmt.Println("管理员已创建，请妥善保存初始密码和主密钥文件。")
		return tx.Create(&app.Audit{UserID: &user.ID, Actor: username, Action: "system.initialize", Result: "success"}).Error
	})
}
