package app

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"testing"
	"time"
)

func TestRolesDoNotCrossProduct(t *testing.T) {
	user := User{Roles: []Role{{Permissions: []string{"instances.reboot"}, Scopes: []Scope{{AccountID: 1}}}, {Permissions: []string{"resources.read"}, Scopes: []Scope{{AccountID: 2}}}}}
	if Can(user, "instances.reboot", &Resource{AccountID: 2}) {
		t.Fatal("跨角色组合扩大了账号 2 的权限")
	}
	if !Can(user, "instances.reboot", &Resource{AccountID: 1}) {
		t.Fatal("应允许重启账号 1")
	}
	if Can(user, "instances.force_reboot", &Resource{AccountID: 1}) {
		t.Fatal("普通重启不能隐含强制重启")
	}
	user.Roles[0].Scopes = []Scope{{AccountID: 1, Kind: "cvm", Region: "ap-beijing", InstanceID: "ins-one"}}
	for _, r := range []Resource{{AccountID: 1, Kind: "lighthouse", Region: "ap-beijing", InstanceID: "ins-one"}, {AccountID: 1, Kind: "cvm", Region: "ap-shanghai", InstanceID: "ins-one"}, {AccountID: 1, Kind: "cvm", Region: "ap-beijing", InstanceID: "ins-other"}} {
		if Can(user, "instances.reboot", &r) {
			t.Fatal("资源范围校验失效")
		}
	}
}
func TestAuthenticatedEncryption(t *testing.T) {
	block, _ := aes.NewCipher(make([]byte, 32))
	key, _ := cipher.NewGCM(block)
	a, e := Encrypt(key, "secret")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := Encrypt(key, "secret")
	if a == b {
		t.Fatal("每次加密应生成不同 nonce")
	}
	plain, e := Decrypt(key, a)
	if e != nil || plain != "secret" {
		t.Fatal("解密失败")
	}
	raw, _ := base64.StdEncoding.DecodeString(a)
	raw[len(raw)-1] ^= 1
	if _, e := Decrypt(key, base64.StdEncoding.EncodeToString(raw)); e == nil {
		t.Fatal("应拒绝篡改密文")
	}
	for _, invalid := range []string{"", "invalid", "YWJj"} {
		if _, e := Decrypt(key, invalid); e == nil {
			t.Fatal("应拒绝无效密文")
		}
	}
}
func TestSessionExpiry(t *testing.T) {
	now := time.Now()
	settings := Settings{IdleMinutes: 30, MaxHours: 8}
	s := Session{CreatedAt: now.Add(-time.Hour), LastSeen: now.Add(-29 * time.Minute)}
	if !SessionValid(s, settings, now) {
		t.Fatal("会话过早失效")
	}
	s.LastSeen = now.Add(-30 * time.Minute)
	if SessionValid(s, settings, now) {
		t.Fatal("空闲超时未失效")
	}
	s.LastSeen = now
	s.CreatedAt = now.Add(-8 * time.Hour)
	if SessionValid(s, settings, now) {
		t.Fatal("最长时长未生效")
	}
}
func TestExplicitStopTypes(t *testing.T) {
	for _, kind := range []string{"cvm", "lighthouse"} {
		for _, action := range []string{"start", "stop", "reboot", "force_stop", "force_reboot"} {
			_, params := OperationParameters(Resource{Kind: kind, InstanceID: "ins-test"}, action)
			expected := "SOFT"
			if action == "force_stop" || action == "force_reboot" {
				expected = "HARD"
			}
			if action == "start" {
				if _, ok := params["StopType"]; ok {
					t.Fatal("启动不应传 StopType")
				}
			} else if params["StopType"] != expected {
				t.Fatalf("%s %s 未显式设置 %s", kind, action, expected)
			}
		}
	}
}
