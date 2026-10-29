package app

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	errAccountHasActiveTasks = errors.New("cloud account has active tasks")
	errDeleteSelf            = errors.New("cannot delete current user")
	errLastActiveAdmin       = errors.New("cannot delete the last active administrator")
)

func adminOnly(c *gin.Context) bool {
	if !IsAdmin(current(c)) {
		fail(c, 403, "此授权变更仅限管理员执行")
		return false
	}
	return true
}
func (a *App) users(c *gin.Context) {
	var rows []User
	if a.DB.Preload("Roles").Order("id").Find(&rows).Error != nil {
		fail(c, 503, "无法读取用户")
		return
	}
	c.JSON(200, rows)
}
func (a *App) saveUser(c *gin.Context) {
	if !adminOnly(c) {
		return
	}
	var in struct {
		Username    string   `json:"username"`
		DisplayName string   `json:"display_name"`
		Enabled     bool     `json:"enabled"`
		RoleIDs     []uint64 `json:"role_ids"`
	}
	if !readJSON(c, &in) {
		return
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_.@-]{3,64}$`).MatchString(in.Username) || len(in.DisplayName) > 128 {
		bad(c, "账号需为 3 至 64 位字母、数字或 _ . @ -")
		return
	}
	var u User
	creating := c.Param("id") == ""
	if !creating && a.DB.First(&u, c.Param("id")).Error != nil {
		fail(c, 404, "用户不存在")
		return
	}
	var roles []Role
	if len(in.RoleIDs) > 0 {
		if a.DB.Where("id IN ?", in.RoleIDs).Find(&roles).Error != nil || len(roles) != len(in.RoleIDs) {
			bad(c, "角色不存在或重复")
			return
		}
	}
	actor := current(c)
	if !creating && u.ID == actor.ID {
		hasAdmin := false
		for _, r := range roles {
			hasAdmin = hasAdmin || r.Administrator
		}
		if !in.Enabled || !hasAdmin {
			bad(c, "不能停用自己或移除自己的管理员角色")
			return
		}
	}
	temp := ""
	if creating {
		temp = RandomToken()[:20]
		hash, e := bcrypt.GenerateFromPassword([]byte(temp), 12)
		if e != nil {
			fail(c, 500, "无法生成密码")
			return
		}
		u.PasswordHash = string(hash)
		u.MustChangePassword = true
		u.Preferences = map[string]any{}
	}
	u.Username = in.Username
	u.DisplayName = in.DisplayName
	u.Enabled = in.Enabled
	if a.mutation(c, "users.save", in.Username, func(tx *gorm.DB) error {
		if e := tx.Omit("Roles").Save(&u).Error; e != nil {
			return e
		}
		if e := tx.Model(&u).Association("Roles").Replace(roles); e != nil {
			return e
		}
		return tx.Where("user_id = ?", u.ID).Delete(&Session{}).Error
	}) {
		c.JSON(200, gin.H{"id": u.ID, "temporary_password": temp})
	}
}

func (a *App) deleteUser(c *gin.Context) {
	if !adminOnly(c) {
		return
	}
	var user User
	if a.DB.First(&user, c.Param("id")).Error != nil {
		fail(c, 404, "用户不存在")
		return
	}
	if user.ID == current(c).ID {
		fail(c, 409, "不能删除当前登录的管理员账号")
		return
	}
	var adminRoleCount int64
	if err := a.DB.Table("user_roles").Joins("JOIN roles ON roles.id = user_roles.role_id").Where("user_roles.user_id = ? AND roles.administrator = true", user.ID).Count(&adminRoleCount).Error; err != nil {
		fail(c, 503, "无法校验管理员账号")
		return
	}
	if user.Enabled && adminRoleCount > 0 {
		var otherAdmins int64
		if err := a.DB.Model(&User{}).Joins("JOIN user_roles ON user_roles.user_id = users.id").Joins("JOIN roles ON roles.id = user_roles.role_id").Where("users.enabled = true AND roles.administrator = true AND users.id <> ?", user.ID).Distinct("users.id").Count(&otherAdmins).Error; err != nil {
			fail(c, 503, "无法校验管理员账号")
			return
		}
		if otherAdmins == 0 {
			fail(c, 409, "不能删除最后一个启用的管理员账号")
			return
		}
	}
	if a.mutation(c, "users.delete", user.Username, func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, user.ID).Error; err != nil {
			return err
		}
		if user.ID == current(c).ID {
			return errDeleteSelf
		}
		var adminRoleCount int64
		if err := tx.Table("user_roles").Joins("JOIN roles ON roles.id = user_roles.role_id").Where("user_roles.user_id = ? AND roles.administrator = true", user.ID).Count(&adminRoleCount).Error; err != nil {
			return err
		}
		if user.Enabled && adminRoleCount > 0 {
			var otherAdmins int64
			if err := tx.Model(&User{}).Joins("JOIN user_roles ON user_roles.user_id = users.id").Joins("JOIN roles ON roles.id = user_roles.role_id").Where("users.enabled = true AND roles.administrator = true AND users.id <> ?", user.ID).Distinct("users.id").Count(&otherAdmins).Error; err != nil {
				return err
			}
			if otherAdmins == 0 {
				return errLastActiveAdmin
			}
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&Session{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&Identity{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&OAuthState{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Audit{}).Where("user_id = ?", user.ID).Update("user_id", nil).Error; err != nil {
			return err
		}
		return tx.Delete(&user).Error
	}) {
		c.Status(204)
	}
}
func (a *App) resetPassword(c *gin.Context) {
	if !adminOnly(c) {
		return
	}
	var u User
	if a.DB.First(&u, c.Param("id")).Error != nil {
		fail(c, 404, "用户不存在")
		return
	}
	temp := RandomToken()[:20]
	hash, e := bcrypt.GenerateFromPassword([]byte(temp), 12)
	if e != nil {
		fail(c, 500, "无法生成密码")
		return
	}
	if a.mutation(c, "users.reset_password", u.Username, func(tx *gorm.DB) error {
		if e := tx.Model(&u).Updates(map[string]any{"password_hash": string(hash), "must_change_password": true}).Error; e != nil {
			return e
		}
		return tx.Where("user_id = ?", u.ID).Delete(&Session{}).Error
	}) {
		c.JSON(200, gin.H{"temporary_password": temp})
	}
}
func (a *App) roles(c *gin.Context) {
	var rows []Role
	if a.DB.Order("id").Find(&rows).Error != nil {
		fail(c, 503, "无法读取角色")
		return
	}
	c.JSON(200, rows)
}
func (a *App) saveRole(c *gin.Context) {
	if !adminOnly(c) {
		return
	}
	var in Role
	if !readJSON(c, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 {
		bad(c, "请输入角色名称")
		return
	}
	for _, p := range in.Permissions {
		if !slices.Contains(Permissions, p) {
			bad(c, "未知权限点")
			return
		}
	}
	for _, s := range in.Scopes {
		var count int64
		if s.AccountID == 0 || a.DB.Model(&CloudAccount{}).Where("id = ?", s.AccountID).Count(&count).Error != nil || count != 1 {
			bad(c, "资源范围必须指定有效云账号")
			return
		}
		if s.Kind != "" && !isServer(s.Kind) && !isDatabase(s.Kind) {
			bad(c, "资源类型无效")
			return
		}
		if s.InstanceID != "" && (s.Region == "" || s.Kind == "") {
			bad(c, "指定实例时必须同时指定地域和资源类型")
			return
		}
	}
	role := Role{}
	if c.Param("id") != "" {
		if a.DB.First(&role, c.Param("id")).Error != nil {
			fail(c, 404, "角色不存在")
			return
		}
		if role.Administrator {
			bad(c, "管理员角色不可修改")
			return
		}
	}
	role.Name = in.Name
	role.Permissions = in.Permissions
	role.Scopes = in.Scopes
	role.Administrator = false
	if a.mutation(c, "roles.save", in.Name, func(tx *gorm.DB) error {
		// 与账号删除按相同顺序锁定账号，避免并发保存失效的授权范围。
		ids := make([]uint64, 0, len(in.Scopes))
		for _, scope := range in.Scopes {
			ids = append(ids, scope.AccountID)
		}
		slices.Sort(ids)
		ids = slices.Compact(ids)
		for _, id := range ids {
			var account CloudAccount
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, id).Error; err != nil {
				return err
			}
		}
		return tx.Save(&role).Error
	}) {
		c.JSON(200, role)
	}
}

func (a *App) deleteRole(c *gin.Context) {
	if !adminOnly(c) {
		return
	}
	var role Role
	if a.DB.First(&role, c.Param("id")).Error != nil {
		fail(c, 404, "角色不存在")
		return
	}
	if role.Administrator {
		bad(c, "管理员角色不可删除")
		return
	}
	if a.mutation(c, "roles.delete", role.Name, func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&role, role.ID).Error; err != nil {
			return err
		}
		if role.Administrator {
			return errors.New("管理员角色不可删除")
		}
		return tx.Delete(&role).Error
	}) {
		c.Status(204)
	}
}
func (a *App) saveAccount(c *gin.Context) {
	var in struct {
		Provider  string `json:"provider"`
		Site      string `json:"site"`
		Name      string `json:"name"`
		SecretID  string `json:"secret_id"`
		SecretKey string `json:"secret_key"`
		Enabled   bool   `json:"enabled"`
	}
	if !readJSON(c, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 {
		bad(c, "请输入不超过 100 字符的名称")
		return
	}
	var ac CloudAccount
	if c.Param("id") != "" && a.DB.First(&ac, c.Param("id")).Error != nil {
		fail(c, 404, "云账号不存在")
		return
	}
	if in.Provider == "" {
		in.Provider = ac.Provider
	}
	if in.Provider == "" {
		in.Provider = "tencent"
	}
	if in.Site == "" {
		in.Site = ac.Site
	}
	if in.Site == "" {
		in.Site = "china"
	}
	if (in.Provider != "tencent" && in.Provider != "alibaba") || (in.Site != "china" && in.Site != "international") {
		bad(c, "云厂商或账号站点无效")
		return
	}
	if ac.ID != 0 && (ac.Provider != in.Provider || ac.Site != in.Site) {
		bad(c, "已有账号不能切换厂商或站点，请新建云账号")
		return
	}
	ac.Name = in.Name
	ac.Provider = in.Provider
	ac.Site = in.Site
	if ac.Regions == nil {
		ac.Regions = []string{}
	}
	ac.Enabled = in.Enabled
	var err error
	if in.SecretID != "" {
		ac.SecretID, err = Encrypt(a.Key, in.SecretID)
		if err != nil {
			fail(c, 500, "凭证加密失败")
			return
		}
	}
	if in.SecretKey != "" {
		ac.SecretKey, err = Encrypt(a.Key, in.SecretKey)
		if err != nil {
			fail(c, 500, "凭证加密失败")
			return
		}
	}
	if ac.SecretID == "" || ac.SecretKey == "" {
		bad(c, "首次接入需提供 Secret ID 和 Secret Key")
		return
	}
	if a.mutation(c, "accounts.save", in.Name, func(tx *gorm.DB) error {
		if e := tx.Save(&ac).Error; e != nil {
			return e
		}
		if ac.Enabled {
			return enqueueSync(tx, ac.ID)
		}
		return nil
	}) {
		c.JSON(200, ac)
	}
}

func (a *App) deleteAccount(c *gin.Context) {
	var account CloudAccount
	if a.DB.First(&account, c.Param("id")).Error != nil {
		fail(c, 404, "云账号不存在")
		return
	}
	resourceIDs := a.DB.Model(&Resource{}).Select("id").Where("account_id = ?", account.ID)
	var active int64
	if err := a.DB.Model(&Task{}).Where("(account_id = ? OR resource_id IN (?)) AND status IN ?", account.ID, resourceIDs, []string{"queued", "submitting", "waiting", "unknown"}).Count(&active).Error; err != nil {
		fail(c, 503, "无法检查云账号任务")
		return
	}
	if active > 0 {
		if err := a.record(c, a.DB, "accounts.delete", account.Name, "failed", "账号仍有未完成任务"); err != nil {
			fail(c, 503, "审计服务不可用")
			return
		}
		fail(c, 409, "账号仍有排队或运行中的任务，完成后才能删除")
		return
	}
	if a.mutation(c, "accounts.delete", account.Name, func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, account.ID).Error; err != nil {
			return err
		}
		resourceIDs := tx.Model(&Resource{}).Select("id").Where("account_id = ?", account.ID)
		var active int64
		if err := tx.Model(&Task{}).Where("(account_id = ? OR resource_id IN (?)) AND status IN ?", account.ID, resourceIDs, []string{"queued", "submitting", "waiting", "unknown"}).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return errAccountHasActiveTasks
		}
		var roles []Role
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Find(&roles).Error; err != nil {
			return err
		}
		for i := range roles {
			scopes := roles[i].Scopes[:0]
			for _, scope := range roles[i].Scopes {
				if scope.AccountID != account.ID {
					scopes = append(scopes, scope)
				}
			}
			if len(scopes) != len(roles[i].Scopes) {
				roles[i].Scopes = scopes
				if err := tx.Save(&roles[i]).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Model(&Task{}).Where("account_id = ? OR resource_id IN (?)", account.ID, resourceIDs).Updates(map[string]any{"account_id": nil, "resource_id": nil}).Error; err != nil {
			return err
		}
		if err := tx.Where("account_id = ?", account.ID).Delete(&Resource{}).Error; err != nil {
			return err
		}
		return tx.Delete(&account).Error
	}) {
		c.Status(204)
	}
}
func (a *App) getSettings(c *gin.Context) {
	s, e := a.settings()
	if e != nil {
		fail(c, 503, "无法读取设置")
		return
	}
	c.JSON(200, s)
}
func (a *App) saveSettings(c *gin.Context) {
	var s Settings
	if !readJSON(c, &s) {
		return
	}
	if s.StaleMinutes < 1 || s.StaleMinutes > 1440 || s.IdleMinutes < 5 || s.IdleMinutes > 1440 || s.MaxHours < 1 || s.MaxHours > 168 || s.RetentionDays < 1 || s.RetentionDays > 3650 {
		bad(c, "数据过期提示 1～1440 分钟，空闲 5～1440 分钟，最长会话 1～168 小时，日志 1～3650 天")
		return
	}
	for _, m := range s.Metrics {
		if !slices.Contains([]string{"cpu", "memory", "disk", "network_in", "network_out"}, m) {
			bad(c, "未知监控指标")
			return
		}
	}
	old, err := a.settings()
	if err != nil {
		fail(c, 503, "无法读取设置")
		return
	}
	if s.RetentionDays < old.RetentionDays && c.GetHeader("X-Confirm-Retention") != "true" {
		bad(c, "缩短保留时间将清理已有日志，请确认")
		return
	}
	s.ID = 1
	if a.mutation(c, "settings.save", "global", func(tx *gorm.DB) error { return tx.Save(&s).Error }) {
		c.JSON(200, s)
	}
}
func (a *App) audits(c *gin.Context) {
	db := a.DB.Model(&Audit{})
	for _, k := range []string{"actor", "action", "result"} {
		if v := c.Query(k); v != "" {
			if k == "result" {
				db = db.Where("COALESCE((SELECT status FROM tasks WHERE audit_id = audits.id ORDER BY id DESC LIMIT 1), CASE WHEN action IN ('accounts.sync','resources.sync') AND result = 'success' THEN 'accepted' ELSE result END) = ?", v)
			} else {
				db = db.Where(k+" = ?", v)
			}
		}
	}
	if v := c.Query("q"); v != "" {
		pattern := "%" + v + "%"
		db = db.Where("target ILIKE ? OR target_label ILIKE ? OR target_details::text ILIKE ? OR message ILIKE ? OR id IN (SELECT audit_id FROM tasks WHERE audit_id IS NOT NULL AND refresh_results::text ILIKE ?)", pattern, pattern, pattern, pattern, pattern)
	}
	for _, f := range []struct{ Query, Op string }{{"from", ">="}, {"to", "<="}} {
		if v := c.Query(f.Query); v != "" {
			t, e := time.Parse(time.RFC3339, v)
			if e != nil {
				bad(c, "日期格式不正确")
				return
			}
			db = db.Where("created_at "+f.Op+" ?", t)
		}
	}
	var count int64
	var rows []Audit
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	if db.Count(&count).Error != nil || db.Order("id DESC").Offset((page-1)*30).Limit(30).Find(&rows).Error != nil {
		fail(c, 503, "无法读取审计日志")
		return
	}
	if presentAudits(a.DB, rows) != nil {
		fail(c, 503, "无法读取审计目标详情")
		return
	}
	c.JSON(200, gin.H{"items": rows, "total": count})
}
func (a *App) identities(c *gin.Context) {
	var rows []Identity
	db := a.DB
	if !IsAdmin(current(c)) {
		db = db.Where("user_id = ?", current(c).ID)
	}
	if db.Find(&rows).Error != nil {
		fail(c, 503, "无法读取关联身份")
		return
	}
	c.JSON(200, rows)
}
func (a *App) unlink(c *gin.Context) {
	if !adminOnly(c) {
		return
	}
	var row Identity
	if a.DB.First(&row, c.Param("id")).Error != nil {
		fail(c, 404, "身份关联不存在")
		return
	}
	var provider OAuthProvider
	var user User
	if a.DB.Select("id, name").First(&provider, row.ProviderID).Error != nil || a.DB.Select("id, username").First(&user, row.UserID).Error != nil {
		fail(c, 503, "无法读取身份关联目标")
		return
	}
	c.Set("audit_target_snapshot", Audit{TargetLabel: provider.Name + " · " + user.Username, TargetDetails: []AuditTarget{{Kind: "identity", Name: provider.Name + " · " + user.Username}}})
	if a.mutation(c, "identities.unlink", fmt.Sprint(row.ID), func(tx *gorm.DB) error {
		if e := tx.Delete(&row).Error; e != nil {
			return e
		}
		return tx.Where("user_id = ?", row.UserID).Delete(&Session{}).Error
	}) {
		c.Status(204)
	}
}

var errInvalid = errors.New("invalid")
