package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (a *App) resource(c *gin.Context, permission string) (Resource, bool) {
	var r Resource
	if a.DB.First(&r, c.Param("id")).Error != nil {
		fail(c, 404, "资源不存在")
		return r, false
	}
	if !Can(current(c), permission, &r) {
		if c.Request.Method != "GET" {
			_ = a.record(c, a.DB, permission, ResourceKey(r), "denied", "无资源权限")
		}
		fail(c, 403, "无权访问此资源")
		return r, false
	}
	return r, true
}
func (a *App) resources(c *gin.Context) {
	u := current(c)
	if !Can(u, "resources.read", nil) {
		fail(c, 403, "没有资源查看权限")
		return
	}
	rules, err := ParseSort(c.Query("sort"))
	if err != nil {
		bad(c, err.Error())
		return
	}
	var rows []Resource
	var accounts []CloudAccount
	if a.DB.Find(&rows).Error != nil || a.DB.Find(&accounts).Error != nil {
		fail(c, 503, "资源数据暂不可用")
		return
	}
	names := map[uint64]string{}
	for _, ac := range accounts {
		names[ac.ID] = ac.Name
	}
	filtered := make([]Resource, 0, len(rows))
	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	for _, r := range rows {
		if !Can(u, "resources.read", &r) {
			continue
		}
		if v := c.Query("account_id"); v != "" && v != fmt.Sprint(r.AccountID) {
			continue
		}
		if v := c.Query("kind"); v != "" && v != r.Kind {
			continue
		}
		if v := c.Query("region"); v != "" && v != r.Region {
			continue
		}
		if v := c.Query("state"); v != "" && v != r.State {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.InstanceID+" "+r.Name+" "+r.PublicIP+" "+r.PrivateIP), q) {
			continue
		}
		r.AccountName = names[r.AccountID]
		a.presentResource(u, &r)
		filtered = append(filtered, r)
	}
	SortResources(filtered, rules)
	total := len(filtered)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	start := total
	if page <= (total+size-1)/size {
		start = (page - 1) * size
	}
	end := min(start+size, total)
	selected, _ := strconv.ParseUint(c.Query("account_id"), 10, 64)
	cache, err := a.resourceCache(u, accounts, selected)
	if err != nil {
		fail(c, 503, "无法读取缓存更新时间")
		return
	}
	c.JSON(200, gin.H{"items": filtered[start:end], "total": total, "page": page, "page_size": size, "cache": cache})
}
func (a *App) presentResource(u User, r *Resource) {
	r.AllowedActions = []string{}
	var account CloudAccount
	accountEnabled := a.DB.First(&account, r.AccountID).Error == nil && account.Enabled
	var count int64
	a.DB.Model(&Task{}).Where("resource_id = ? AND status IN ?", r.ID, []string{"queued", "submitting", "waiting", "unknown"}).Count(&count)
	if count == 0 && accountEnabled {
		for _, action := range []string{"start", "stop", "reboot", "force_stop", "force_reboot"} {
			if Can(u, "instances."+action, r) && ((action == "start" && r.State == "STOPPED") || (action != "start" && r.State == "RUNNING")) {
				r.AllowedActions = append(r.AllowedActions, action)
			}
		}
	} else {
		var active Task
		if a.DB.Where("resource_id = ? AND status IN ?", r.ID, []string{"queued", "submitting", "waiting", "unknown"}).Order("id DESC").First(&active).Error == nil {
			r.OperationStatus = active.Status
		}
	}
	if !Can(u, "monitor.read", r) {
		r.Metrics = map[string]Metric{}
		return
	}
	settings, err := a.settings()
	if err != nil {
		r.Metrics = map[string]Metric{}
		return
	}
	enabled := map[string]bool{}
	for _, m := range settings.Metrics {
		enabled[m] = true
	}
	for key, m := range r.Metrics {
		base := key
		if strings.HasPrefix(key, "disk:") {
			base = "disk"
		}
		if !enabled[base] {
			delete(r.Metrics, key)
			continue
		}
		if m.SampledAt != nil && time.Since(*m.SampledAt) > 5*time.Minute {
			m.Status = "stale"
			m.Reason = "数据过期"
			r.Metrics[key] = m
		}
	}
}
func (a *App) resourceDetail(c *gin.Context) {
	r, ok := a.resource(c, "resources.read")
	if !ok {
		return
	}
	u := current(c)
	a.presentResource(u, &r)
	var ac CloudAccount
	a.DB.First(&ac, r.AccountID)
	r.AccountName = ac.Name
	var tasks []Task
	if a.DB.Where("resource_id = ?", r.ID).Order("id desc").Limit(30).Find(&tasks).Error != nil {
		fail(c, 503, "操作记录暂不可用")
		return
	}
	cache, err := a.resourceCache(u, []CloudAccount{ac}, ac.ID)
	if err != nil {
		fail(c, 503, "无法读取缓存更新时间")
		return
	}
	c.JSON(200, gin.H{"resource": r, "tasks": tasks, "cache": cache})
}
func (a *App) action(c *gin.Context) {
	var in struct {
		Action  string `json:"action"`
		Confirm string `json:"confirm"`
	}
	if !readJSON(c, &in) {
		return
	}
	switch in.Action {
	case "start", "stop", "reboot", "force_stop", "force_reboot":
	default:
		bad(c, "不支持的操作")
		return
	}
	r, ok := a.resource(c, "instances."+in.Action)
	if !ok {
		return
	}
	var account CloudAccount
	if a.DB.First(&account, r.AccountID).Error != nil || !account.Enabled {
		bad(c, "云账号已停用，无法执行操作")
		return
	}
	if strings.HasPrefix(in.Action, "force_") && in.Confirm != "确认" {
		bad(c, "请输入“确认”后才能执行强制操作")
		return
	}
	if (in.Action == "start" && r.State != "STOPPED") || (in.Action != "start" && r.State != "RUNNING") {
		bad(c, "当前实例状态不支持此操作")
		return
	}
	u := current(c)
	audit := Audit{UserID: &u.ID, Actor: u.Username, SourceIP: c.ClientIP(), Action: "instances." + in.Action, Target: ResourceKey(r), Result: "accepted", RequestID: c.GetString("request_id")}
	task := Task{ResourceID: &r.ID, AccountID: &r.AccountID, Action: in.Action, Status: "queued", BaselineState: r.State, NextRun: time.Now()}
	err := a.DB.Transaction(func(tx *gorm.DB) error {
		// 唯一索引使同一实例同一时间只能存在一个未完成操作。
		if e := tx.Create(&audit).Error; e != nil {
			return e
		}
		task.AuditID = &audit.ID
		return tx.Create(&task).Error
	})
	if err != nil {
		_ = a.record(c, a.DB, audit.Action, audit.Target, "failed", "已有操作正在执行或无法写入任务")
		fail(c, 409, "已有操作正在执行，或任务暂时无法创建")
		return
	}
	c.JSON(202, task)
}
func (a *App) accounts(c *gin.Context) {
	u := current(c)
	var rows []CloudAccount
	if a.DB.Order("id").Find(&rows).Error != nil {
		fail(c, 503, "无法读取云账号")
		return
	}
	out := []CloudAccount{}
	for _, r := range rows {
		allowed := Can(u, "accounts.manage", nil)
		for _, role := range u.Roles {
			for _, s := range role.Scopes {
				if s.AccountID == r.ID {
					allowed = true
				}
			}
		}
		if allowed {
			out = append(out, r)
		}
	}
	c.JSON(200, out)
}
func (a *App) syncAccount(c *gin.Context) {
	var ac CloudAccount
	if a.DB.First(&ac, c.Param("id")).Error != nil {
		fail(c, 404, "云账号不存在")
		return
	}
	if !ac.Enabled {
		bad(c, "云账号已停用")
		return
	}
	if a.mutation(c, "accounts.sync", fmt.Sprint(ac.ID), func(tx *gorm.DB) error { return enqueueSync(tx, ac.ID) }) {
		c.Status(202)
	}
}
func enqueueSync(tx *gorm.DB, id uint64) error {
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Task{AccountID: &id, Action: "sync", Status: "queued", NextRun: time.Now()}).Error
}
