package main

import (
	"context"
	"crypto/cipher"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/gorm"

	"opsdeck/internal/app"
	"opsdeck/migrations"
)

func waitDatabase(parent context.Context, dsn string) error {
	ctx, cancel := context.WithTimeout(parent, time.Minute)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return errors.New("数据库连接配置无效")
	}
	defer db.Close()
	slog.Info("等待数据库就绪")
	for {
		attempt, stop := context.WithTimeout(ctx, 5*time.Second)
		err = db.PingContext(attempt)
		stop()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("等待数据库就绪失败，请检查数据库连接配置和服务状态")
		case <-time.After(time.Second):
		}
	}
}

func migrateDatabase(dsn string) error {
	source, err := iofs.New(migrations.Files, ".")
	if err != nil {
		return err
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, dsn)
	if err != nil {
		_ = source.Close()
		return errors.New("数据库迁移连接失败")
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	slog.Info("数据库迁移已就绪")
	return nil
}

func bootstrap(db *gorm.DB, keyPath string) (cipher.AEAD, error) {
	var key cipher.AEAD
	var username, password string
	err := db.Transaction(func(tx *gorm.DB) error {
		// 串行执行首次初始化，避免并发启动时重复创建密钥或管理员。
		if err := tx.Exec("SELECT pg_advisory_xact_lock(684304855273133)").Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&app.User{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			// 已部署的数据库必须使用原密钥，禁止重置账号。
			var err error
			key, err = app.OpenKey(tx, keyPath, false)
			return err
		}
		username, password = initialCredentials()
		var err error
		key, err = app.OpenKey(tx, keyPath, true)
		if err != nil {
			return err
		}
		if err := initializeAdmin(tx, username, password); err != nil {
			return err
		}
		return nil
	})
	if err == nil && password != "" {
		logInitialCredentials(username, password)
	}
	return key, err
}
