package app

import (
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type syncProgress struct {
	ID      uint64          `json:"id"`
	Status  string          `json:"status"`
	Error   string          `json:"error"`
	Results []RefreshResult `json:"results"`
}

func accountSyncProgress(u User, task Task) syncProgress {
	message := task.Error
	if message != "" && !Can(u, "accounts.manage", nil) {
		// 账号同步覆盖完整清单，详情可能包含超出查看者资源范围的实例。
		message = "云账号同步失败，请联系管理员查看详细日志"
	}
	return syncProgress{ID: task.ID, Status: task.Status, Error: message}
}

func canRefreshAccount(u User, accountID uint64) bool {
	for _, role := range u.Roles {
		if role.Administrator {
			return true
		}
		if !slices.Contains(role.Permissions, "resources.read") {
			continue
		}
		// 刷新只触发账号快照更新；权限和账号范围仍须来自同一角色。
		for _, scope := range role.Scopes {
			if scope.AccountID == accountID {
				return true
			}
		}
	}
	return false
}

func (a *App) refreshResources(c *gin.Context) {
	var in struct {
		AccountID   uint64   `json:"account_id"`
		ResourceID  uint64   `json:"resource_id"`
		ResourceIDs []uint64 `json:"resource_ids"`
	}
	if !readJSON(c, &in) {
		return
	}
	u := current(c)
	if !Can(u, "resources.read", nil) {
		fail(c, 403, "没有资源查看权限")
		return
	}
	if in.ResourceIDs != nil || in.ResourceID != 0 {
		if in.ResourceID != 0 {
			in.ResourceIDs = append(in.ResourceIDs, in.ResourceID)
		}
		a.refreshSelected(c, in.ResourceIDs)
		return
	}
	if in.AccountID != 0 && !canRefreshAccount(u, in.AccountID) {
		fail(c, 403, "无权刷新此云账号")
		return
	}
	var accounts []CloudAccount
	q := a.DB.Where("enabled = true")
	if in.AccountID != 0 {
		q = q.Where("id = ?", in.AccountID)
	}
	if q.Find(&accounts).Error != nil {
		fail(c, 503, "无法读取云账号")
		return
	}
	snapshot := Audit{TargetDetails: []AuditTarget{}}
	for _, ac := range accounts {
		if canRefreshAccount(u, ac.ID) {
			snapshot.TargetDetails = append(snapshot.TargetDetails, AuditTarget{Kind: "account", Name: ac.Name, AccountName: ac.Name, AccountID: ac.ID})
		}
	}
	snapshot.TargetLabel = auditTargetLabel(snapshot.TargetDetails)
	c.Set("audit_target_snapshot", snapshot)
	out := []syncProgress{}
	if a.mutation(c, "resources.sync", "authorized-accounts", func(tx *gorm.DB) error {
		for _, ac := range accounts {
			if !canRefreshAccount(u, ac.ID) {
				continue
			}
			// 活跃任务唯一索引合并多个页面或用户发起的相同账号同步。
			if err := enqueueSync(tx, ac.ID); err != nil {
				return err
			}
			var task Task
			if err := tx.Where("account_id = ? AND action = 'sync'", ac.ID).Order("id DESC").First(&task).Error; err != nil {
				return err
			}
			out = append(out, accountSyncProgress(u, task))
		}
		return nil
	}) {
		c.JSON(202, gin.H{"tasks": out})
	}
}

func (a *App) syncTasks(c *gin.Context) {
	parts := strings.Split(c.Query("ids"), ",")
	if len(parts) > 100 {
		bad(c, "一次最多查询 100 个同步任务")
		return
	}
	ids := []uint64{}
	for _, part := range parts {
		id, err := strconv.ParseUint(part, 10, 64)
		if err != nil || id == 0 {
			bad(c, "无效同步任务编号")
			return
		}
		ids = append(ids, id)
	}
	var tasks []Task
	if a.DB.Where("id IN ? AND action IN ?", ids, []string{"sync", "monitor"}).Find(&tasks).Error != nil {
		fail(c, 503, "无法读取同步状态")
		return
	}
	out := []syncProgress{}
	for _, task := range tasks {
		if task.Action == "monitor" {
			var resources []Resource
			if a.DB.Where("id IN ?", task.ResourceIDs).Find(&resources).Error != nil {
				fail(c, 503, "无法读取刷新范围")
				return
			}
			if len(resources) != len(task.ResourceIDs) {
				fail(c, 404, "刷新资源已删除")
				return
			}
			for _, r := range resources {
				if !canRefreshResource(current(c), r) {
					fail(c, 403, "无权查看刷新状态")
					return
				}
			}
			out = append(out, syncProgress{ID: task.ID, Status: task.Status, Error: task.Error, Results: task.RefreshResults})
			continue
		}
		if task.AccountID == nil || (!Can(current(c), "accounts.manage", nil) && !canRefreshAccount(current(c), *task.AccountID)) {
			fail(c, 403, "无权查看同步状态")
			return
		}
		out = append(out, accountSyncProgress(current(c), task))
	}
	if len(tasks) != len(slices.Compact(slices.Sorted(slices.Values(ids)))) {
		fail(c, 404, "同步任务不存在或已过期")
		return
	}
	c.JSON(200, gin.H{"tasks": out})
}
