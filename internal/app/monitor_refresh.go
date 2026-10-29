package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RefreshResult struct {
	ResourceID uint64 `json:"resource_id"`
	Name       string `json:"name"`
	InstanceID string `json:"instance_id"`
	Status     string `json:"status"`
	Error      string `json:"error"`
}

func canRefreshResource(u User, r Resource) bool {
	for _, role := range u.Roles {
		oneRole := User{Roles: []Role{role}}
		if Can(oneRole, "resources.read", &r) && Can(oneRole, "monitor.read", &r) {
			return true
		}
	}
	return false
}

func (a *App) refreshSelected(c *gin.Context, ids []uint64) {
	if len(ids) == 0 || len(ids) > 100 || slices.Contains(ids, uint64(0)) {
		bad(c, "请选择 1 至 100 台实例")
		return
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	var resources []Resource
	if a.DB.Where("id IN ?", ids).Order("account_id, id").Find(&resources).Error != nil {
		fail(c, 503, "无法读取刷新范围")
		return
	}
	if len(resources) != len(ids) {
		fail(c, 404, "部分实例已不存在，请重新加载列表")
		return
	}
	u := current(c)
	groups := map[uint64][]uint64{}
	accountIDs := []uint64{}
	for _, r := range resources {
		if !canRefreshResource(u, r) {
			_ = a.record(c, a.DB, "resources.monitor", "selected-resources", "denied", "无权刷新所选实例监控")
			fail(c, 403, "无权刷新所选实例的监控")
			return
		}
		if len(groups[r.AccountID]) == 0 {
			accountIDs = append(accountIDs, r.AccountID)
		}
		groups[r.AccountID] = append(groups[r.AccountID], r.ID)
	}
	out := []syncProgress{}
	err := a.DB.Transaction(func(tx *gorm.DB) error {
		for _, id := range accountIDs {
			var ac CloudAccount
			// 与账号删除串行，并合并相同实例集合的活跃刷新任务。
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&ac, id).Error; err != nil {
				return err
			}
			if !ac.Enabled {
				return errors.New("account disabled")
			}
			encoded, _ := json.Marshal(groups[id])
			var task Task
			err := tx.Where("account_id = ? AND action = 'monitor' AND status IN ? AND resource_ids = ?::jsonb", id, []string{"queued", "submitting"}, string(encoded)).First(&task).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				audit := Audit{UserID: &u.ID, Actor: u.Username, SourceIP: sourceIP(c), Action: "resources.monitor", Target: string(encoded), Result: "accepted", RequestID: c.GetString("request_id")}
				if err = fillAuditTarget(tx, &audit); err != nil {
					return err
				}
				if err = tx.Create(&audit).Error; err != nil {
					return err
				}
				task = Task{AccountID: &id, AuditID: &audit.ID, ResourceIDs: groups[id], Action: "monitor", Status: "queued", NextRun: time.Now()}
				if err = tx.Create(&task).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			out = append(out, syncProgress{ID: task.ID, Status: task.Status, Results: task.RefreshResults})
		}
		return nil
	})
	if err != nil {
		_ = a.record(c, a.DB, "resources.monitor", "selected-resources", "failed", "刷新任务创建失败")
		fail(c, 409, "无法创建刷新任务，请检查云账号是否启用")
		return
	}
	c.Set("audited", true)
	c.JSON(202, gin.H{"tasks": out})
}

func (a *App) refreshMonitor(ctx context.Context, task Task) {
	var resources []Resource
	if a.DB.Where("id IN ?", task.ResourceIDs).Order("id").Find(&resources).Error != nil {
		a.finish(task, "failed", "无法读取实例快照", "")
		return
	}
	var ac CloudAccount
	var audit Audit
	var actor User
	allowed := task.AccountID != nil && a.DB.First(&ac, task.AccountID).Error == nil && ac.Enabled &&
		task.AuditID != nil && a.DB.First(&audit, task.AuditID).Error == nil && audit.UserID != nil &&
		a.DB.Preload("Roles").First(&actor, audit.UserID).Error == nil && actor.Enabled
	settings, err := a.settings()
	if err != nil {
		a.finish(task, "failed", "无法读取监控设置", "")
		return
	}
	results := make([]RefreshResult, len(resources))
	groups := map[string][]int{}
	for i, r := range resources {
		results[i] = RefreshResult{ResourceID: r.ID, Name: r.Name, InstanceID: r.InstanceID, Status: "failed"}
		if !allowed || r.AccountID != ac.ID || !canRefreshResource(actor, r) {
			results[i].Error = "云账号已停用或刷新发起人的授权已失效"
			continue
		}
		key := r.Region + "/" + r.Kind
		groups[key] = append(groups[key], i)
	}
	if a.DB.Model(&task).Update("status", "submitting").Error != nil {
		return
	}
	var pending sync.WaitGroup
	slots := make(chan struct{}, 4)
	for _, indexes := range groups {
		slots <- struct{}{}
		pending.Add(1)
		go func(indexes []int) {
			defer pending.Done()
			defer func() { <-slots }()
			first := resources[indexes[0]]
			// 只查询所选地域和产品，刷新状态、规格及监控查询所需的产品上下文。
			fresh, err := a.Cloud.List(ctx, ac, first.Region, first.Kind)
			if err != nil {
				for _, i := range indexes {
					results[i].Error = "资源查询失败：" + err.Error()
				}
				return
			}
			byID := map[string]Resource{}
			for _, r := range fresh {
				if r.AccountID == ac.ID && r.Region == first.Region && r.Kind == first.Kind {
					byID[r.InstanceID] = r
				}
			}
			selected := []Resource{}
			for _, i := range indexes {
				if r, ok := byID[resources[i].InstanceID]; ok {
					selected = append(selected, r)
				}
			}
			enabled := settings.Metrics
			if isDatabase(first.Kind) {
				enabled = []string{"cpu", "memory", "disk", "connections"}
			}
			metrics := a.Cloud.MonitorBatch(ctx, ac, selected, enabled)
			for _, i := range indexes {
				old := resources[i]
				r, exists := byID[old.InstanceID]
				if !exists {
					results[i].Error = "云端未返回该实例，请同步云账号后重试"
					continue
				}
				if r.State != "RUNNING" || len(enabled) == 0 {
					results[i].Status, results[i].Error = "skipped", "实例未运行或未启用监控指标"
					if a.DB.Model(&old).Updates(map[string]any{"state": r.State, "synced_at": r.SyncedAt}).Error != nil {
						results[i].Status, results[i].Error = "failed", "无法保存实例状态"
					}
					continue
				}
				values := metrics[r.InstanceID]
				reasons := []string{}
				good := false
				for _, key := range enabled {
					m := values[key]
					good = good || (m.Value != nil && m.SampledAt != nil && m.Status == "ok")
					if m.Status == "error" {
						reason := m.Reason
						if reason == "" {
							reason = "云监控请求失败"
						}
						if !slices.Contains(reasons, reason) {
							reasons = append(reasons, reason)
						}
					}
				}
				if len(reasons) > 0 || !good {
					if len(reasons) == 0 {
						reasons = append(reasons, "云监控未返回有效采样，请检查指标可用条件")
					}
					results[i].Error = strings.Join(reasons, "；")
					continue
				}
				// 失败实例不覆盖快照；成功实例也不把更旧的采样覆盖到已有指标上。
				for key, previous := range old.Metrics {
					current := values[key]
					if previous.Value != nil && previous.SampledAt != nil && (current.SampledAt == nil || current.SampledAt.Before(*previous.SampledAt)) {
						values[key] = previous
					}
				}
				r.ID, r.Metrics = old.ID, values
				refreshedAt := time.Now()
				r.MonitorRefreshedAt = &refreshedAt
				if err := a.DB.Model(&r).Select("Name", "PublicIP", "PrivateIP", "Spec", "CPU", "Memory", "Disk", "State", "Tags", "TagsError", "Metrics", "Database", "SyncedAt", "MonitorRefreshedAt").Updates(&r).Error; err != nil {
					results[i].Error = "无法保存监控快照"
					continue
				}
				results[i].Status = "succeeded"
			}
		}(indexes)
	}
	pending.Wait()
	failed := 0
	for _, r := range results {
		if r.Status == "failed" {
			failed++
			slog.ErrorContext(ctx, "instance monitor refresh failed", "task_id", task.ID, "account_id", task.AccountID, "resource_id", r.ResourceID, "instance_id", r.InstanceID, "detail", r.Error)
		}
	}
	task.RefreshResults, task.Status, task.Error = results, "succeeded", ""
	if failed > 0 || len(resources) != len(task.ResourceIDs) {
		task.Status = "failed"
		task.Error = fmt.Sprintf("%d 台实例刷新失败", failed+len(task.ResourceIDs)-len(resources))
	}
	task.LeaseUntil, task.UpdatedAt = nil, time.Now()
	err = a.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&task).Select("RefreshResults", "Status", "Error", "LeaseUntil", "UpdatedAt").Updates(&task).Error; err != nil {
			return err
		}
		if task.AuditID != nil {
			return tx.Model(&Audit{}).Where("id = ?", task.AuditID).Updates(map[string]any{"result": task.Status, "message": task.Error}).Error
		}
		return nil
	})
	if err != nil {
		slog.Error("monitor refresh persist failed", "task_id", task.ID)
	}
}
