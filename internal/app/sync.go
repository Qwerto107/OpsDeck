package app

import (
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type syncProgress struct {
	ID     uint64 `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error"`
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
		AccountID  uint64 `json:"account_id"`
		ResourceID uint64 `json:"resource_id"`
	}
	if !readJSON(c, &in) {
		return
	}
	u := current(c)
	if !Can(u, "resources.read", nil) {
		fail(c, 403, "没有资源查看权限")
		return
	}
	if in.ResourceID != 0 {
		var r Resource
		if a.DB.First(&r, in.ResourceID).Error != nil {
			fail(c, 404, "资源不存在")
			return
		}
		if !Can(u, "resources.read", &r) {
			fail(c, 403, "无权刷新此资源")
			return
		}
		in.AccountID = r.AccountID
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
			out = append(out, syncProgress{task.ID, task.Status, task.Error})
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
	if a.DB.Where("id IN ? AND action = 'sync'", ids).Find(&tasks).Error != nil {
		fail(c, 503, "无法读取同步状态")
		return
	}
	out := []syncProgress{}
	for _, task := range tasks {
		if task.AccountID == nil || !canRefreshAccount(current(c), *task.AccountID) {
			fail(c, 403, "无权查看同步状态")
			return
		}
		out = append(out, syncProgress{task.ID, task.Status, task.Error})
	}
	if len(tasks) != len(slices.Compact(slices.Sorted(slices.Values(ids)))) {
		fail(c, 404, "同步任务不存在或已过期")
		return
	}
	c.JSON(200, gin.H{"tasks": out})
}
