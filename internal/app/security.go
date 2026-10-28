package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
)

func RandomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func TokenHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func OpenKey(db *gorm.DB, path string, initialize bool) (cipher.AEAD, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && initialize {
		var count int64
		for _, table := range []string{"key_checks", "cloud_accounts", "oauth_providers"} {
			if e := db.Table(table).Count(&count).Error; e != nil {
				return nil, e
			}
			if count > 0 {
				return nil, errors.New("数据库已有加密数据或密钥校验记录，不能重新生成主密钥")
			}
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		data = make([]byte, 32)
		if _, err = rand.Read(data); err != nil {
			return nil, err
		}
		// 独占创建防止首次初始化并发覆盖主密钥。
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, e
		}
		_, err = f.Write(data)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return nil, fmt.Errorf("读取主密钥失败（首次部署运行 init）：%w", err)
	}
	if len(data) != 32 {
		return nil, errors.New("主密钥必须为 32 字节")
	}
	block, err := aes.NewCipher(data)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	var check KeyCheck
	err = db.First(&check, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if !initialize {
			return nil, errors.New("尚未初始化密钥校验记录")
		}
		value, e := Encrypt(aead, "opsdeck-key-check-v1")
		if e != nil {
			return nil, e
		}
		if e = db.Create(&KeyCheck{ID: 1, Value: value}).Error; e != nil {
			return nil, e
		}
	} else if err != nil {
		return nil, err
	} else if plain, e := Decrypt(aead, check.Value); e != nil || plain != "opsdeck-key-check-v1" {
		return nil, errors.New("主密钥不匹配，拒绝启动")
	}
	return aead, nil
}
func Encrypt(a cipher.AEAD, s string) (string, error) {
	n := make([]byte, a.NonceSize())
	if _, e := rand.Read(n); e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(a.Seal(n, n, []byte(s), []byte("opsdeck-v1"))), nil
}
func Decrypt(a cipher.AEAD, s string) (string, error) {
	b, e := base64.StdEncoding.DecodeString(s)
	if e != nil || len(b) < a.NonceSize() {
		return "", errors.New("无效密文")
	}
	p, e := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte("opsdeck-v1"))
	return string(p), e
}

var Permissions = []string{"resources.read", "monitor.read", "instances.start", "instances.stop", "instances.reboot", "instances.force_stop", "instances.force_reboot", "users.manage", "roles.manage", "accounts.manage", "audit.read", "settings.manage"}

func IsAdmin(u User) bool {
	for _, r := range u.Roles {
		if r.Administrator {
			return true
		}
	}
	return false
}
func Can(u User, permission string, resource *Resource) bool {
	for _, role := range u.Roles {
		if role.Administrator {
			return true
		}
		if !slices.Contains(role.Permissions, permission) {
			continue
		}
		if resource == nil {
			return true
		}
		// 功能权限与资源范围必须来自同一角色，禁止将多个角色做笛卡尔积。
		for _, s := range role.Scopes {
			if s.AccountID == resource.AccountID && (s.Kind == "" || s.Kind == resource.Kind) && (s.Region == "" || s.Region == resource.Region) && (s.InstanceID == "" || s.InstanceID == resource.InstanceID) {
				return true
			}
		}
	}
	return false
}
func ResourceKey(r Resource) string {
	return fmt.Sprintf("%s/%d/%s/%s/%s", r.Provider, r.AccountID, r.Kind, r.Region, r.InstanceID)
}
func SessionValid(s Session, settings Settings, now time.Time) bool {
	return now.Sub(s.LastSeen) < time.Duration(settings.IdleMinutes)*time.Minute && now.Sub(s.CreatedAt) < time.Duration(settings.MaxHours)*time.Hour
}
func ValidPassword(s string) bool { return len(s) >= 12 && len(s) <= 72 && strings.TrimSpace(s) == s }
