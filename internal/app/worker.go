package app

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (a *App) RunWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	nextCleanup := time.Time{}
	for {
		if ctx.Err() != nil {
			return
		}
		if time.Now().After(nextCleanup) {
			a.cleanup()
			nextCleanup = time.Now().Add(time.Hour)
		}
		var task Task
		err := a.DB.Transaction(func(tx *gorm.DB) error {
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status IN ? AND next_run <= now() AND (lease_until IS NULL OR lease_until < now())", []string{"queued", "submitting", "waiting", "unknown"}).Order("next_run,id").First(&task).Error; e != nil {
				return e
			}
			lease := time.Now().Add(6 * time.Minute)
			task.LeaseUntil = &lease
			return tx.Model(&task).Updates(map[string]any{"lease_until": lease}).Error
		})
		if err == nil {
			a.process(ctx, task)
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			slog.Error("worker claim failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (a *App) cleanup() {
	s, e := a.settings()
	if e != nil {
		return
	}
	e = a.DB.Transaction(func(tx *gorm.DB) error {
		cutoff := time.Now().Add(-time.Duration(s.RetentionDays) * 24 * time.Hour)
		for _, q := range []struct {
			SQL string
			Arg any
		}{{"DELETE FROM audits WHERE created_at < ?", cutoff}, {"DELETE FROM tasks WHERE status IN ('succeeded','failed') AND updated_at < ?", cutoff}, {"DELETE FROM oauth_states WHERE expires_at < ?", time.Now()}, {"DELETE FROM login_attempts WHERE expires_at < ?", time.Now()}, {"DELETE FROM sessions WHERE created_at < ?", time.Now().Add(-time.Duration(s.MaxHours) * time.Hour)}} {
			if e := tx.Exec(q.SQL, q.Arg).Error; e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		slog.Error("retention cleanup failed")
	}
}
func (a *App) finish(task Task, status, message, requestID string) {
	e := a.DB.Transaction(func(tx *gorm.DB) error {
		values := map[string]any{"status": status, "error": message, "lease_until": nil, "next_run": time.Now().Add(15 * time.Second), "updated_at": time.Now()}
		if requestID != "" {
			values["request_id"] = requestID
		}
		if e := tx.Model(&task).Updates(values).Error; e != nil {
			return e
		}
		if task.AuditID != nil {
			result := status
			if status == "waiting" {
				result = "accepted"
			}
			if e := tx.Model(&Audit{}).Where("id = ?", *task.AuditID).Updates(map[string]any{"result": result, "message": message, "request_id": requestID}).Error; e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		slog.Error("task state persist failed", "task_id", task.ID)
	}
}
func (a *App) process(parent context.Context, task Task) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	var ac CloudAccount
	if task.AccountID == nil || a.DB.First(&ac, *task.AccountID).Error != nil {
		a.finish(task, "failed", "云账号不存在或已停用", task.RequestID)
		return
	}
	if !ac.Enabled {
		if task.Action != "sync" && task.Status != "queued" {
			// 停用账号不代表云端操作失败，保留未决任务以阻止重复操作。
			a.finish(task, "unknown", "云账号已停用，恢复后继续核对云端结果", task.RequestID)
		} else {
			a.finish(task, "failed", "云账号已停用", task.RequestID)
		}
		return
	}
	if task.Action == "sync" {
		a.syncResources(ctx, task, ac)
		return
	}
	var r Resource
	if task.ResourceID == nil || a.DB.First(&r, *task.ResourceID).Error != nil {
		a.finish(task, "failed", "实例记录不存在", task.RequestID)
		return
	}
	state, e := a.Cloud.State(ctx, ac, r)
	if e != nil {
		if task.Status == "queued" {
			a.finish(task, "failed", "无法校验云端实例状态", task.RequestID)
		} else {
			a.finish(task, "unknown", "无法查询云端结果，将继续核对", task.RequestID)
		}
		return
	}
	a.DB.Model(&r).Updates(map[string]any{"state": state.State, "synced_at": time.Now()})
	if task.Status == "queued" {
		var audit Audit
		var actor User
		if task.AuditID == nil || a.DB.First(&audit, *task.AuditID).Error != nil || audit.UserID == nil || a.DB.Preload("Roles").First(&actor, *audit.UserID).Error != nil || !actor.Enabled || !Can(actor, "instances."+task.Action, &r) {
			a.finish(task, "failed", "操作发起人的授权已失效", "")
			return
		}
		expected := "RUNNING"
		if task.Action == "start" {
			expected = "STOPPED"
		}
		if state.State != expected {
			a.finish(task, "failed", "云端实例状态已变化，操作取消", "")
			return
		}
		// 在调用云 API 前持久化 submitting。进程崩溃后只查询，不再提交。
		if a.DB.Model(&task).Updates(map[string]any{"status": "submitting", "updated_at": time.Now()}).Error != nil {
			return
		}
		id, err := a.Cloud.Execute(ctx, ac, r, task.Action)
		if err != nil {
			var ce *CloudError
			if errors.As(err, &ce) && ce.Definite {
				a.finish(task, "failed", ce.Code, ce.RequestID)
			} else {
				a.finish(task, "unknown", "请求结果不明，将继续核对；不会自动重复提交", id)
			}
			return
		}
		a.finish(task, "waiting", "", id)
		return
	}
	if state.State != task.BaselineState {
		task.ObservedTransition = true
		if a.DB.Model(&task).Update("observed_transition", true).Error != nil {
			return
		}
	}
	desired := "RUNNING"
	if task.Action == "stop" || task.Action == "force_stop" {
		desired = "STOPPED"
	}
	linked := task.RequestID != "" && state.RequestID == task.RequestID
	if linked && state.OperationState == "FAILED" {
		a.finish(task, "failed", "云端操作失败", task.RequestID)
		return
	}
	reboot := task.Action == "reboot" || task.Action == "force_reboot"
	if state.State == desired && (!reboot || task.ObservedTransition || (linked && state.OperationState == "SUCCESS")) {
		a.finish(task, "succeeded", "", task.RequestID)
		return
	}
	status := "waiting"
	message := ""
	if task.Status == "unknown" || task.Status == "submitting" || time.Since(task.CreatedAt) > 10*time.Minute {
		status = "unknown"
		message = "结果待确认，后台持续核对，不自动重新提交"
	}
	a.finish(task, status, message, task.RequestID)
}
func (a *App) syncResources(ctx context.Context, task Task, ac CloudAccount) {
	settings, e := a.settings()
	if e != nil {
		a.finish(task, "failed", "无法读取监控设置", "")
		return
	}
	if e = a.DB.Model(&task).Update("status", "submitting").Error; e != nil {
		return
	}
	var errorsFound atomic.Bool
	discovered := map[string]bool{}
	discoveryFailed := false
	var pending sync.WaitGroup
	slots := make(chan struct{}, 4)
	// 资源类型由云适配层提供；通用调度层不依赖腾讯云产品名称。
	for _, kind := range a.Cloud.Kinds(ac) {
		regions, err := a.Cloud.Regions(ctx, ac, kind)
		if err != nil {
			errorsFound.Store(true)
			discoveryFailed = true
			continue
		}
	regionsLoop:
		for _, region := range regions {
			discovered[region] = true
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				errorsFound.Store(true)
				discoveryFailed = true
				break regionsLoop
			}
			pending.Add(1)
			go func(region, kind string) {
				defer pending.Done()
				defer func() { <-slots }()
				if err := a.syncRegion(ctx, ac, region, kind, settings.Metrics); err != nil {
					errorsFound.Store(true)
				}
			}(region, kind)
		}
	}
	pending.Wait()
	// 部分发现失败时保留旧地域；不把查询失败误认为资源或地域已经消失。
	if discoveryFailed {
		for _, region := range ac.Regions {
			discovered[region] = true
		}
	}
	ac.Regions = []string{}
	for region := range discovered {
		ac.Regions = append(ac.Regions, region)
	}
	sort.Strings(ac.Regions)
	if a.DB.Model(&ac).Select("Regions").Updates(&ac).Error != nil {
		errorsFound.Store(true)
	}
	message := ""
	status := "succeeded"
	if errorsFound.Load() {
		message = "地域发现或部分资源同步失败，请检查云账号的地域查询、资源查询权限或云接口状态"
		status = "failed"
	}
	a.DB.Model(&ac).Updates(map[string]any{"last_sync": time.Now(), "sync_error": message})
	a.finish(task, status, message, "")
}

func (a *App) syncRegion(ctx context.Context, ac CloudAccount, region, kind string, enabled []string) error {
	rows, err := a.Cloud.List(ctx, ac, region, kind)
	if err != nil {
		return err
	}
	metrics := a.Cloud.MonitorBatch(ctx, ac, rows, enabled)
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.InstanceID)
		r.Metrics = metrics[r.InstanceID]
		if r.Metrics == nil {
			r.Metrics = map[string]Metric{}
		}
		if err := a.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "account_id"}, {Name: "kind"}, {Name: "region"}, {Name: "instance_id"}}, DoUpdates: clause.AssignmentColumns([]string{"name", "public_ip", "private_ip", "spec", "cpu", "memory", "disk", "state", "tags", "tags_error", "metrics", "synced_at"})}).Create(&r).Error; err != nil {
			return err
		}
	}
	// 列表必须完整查询并成功保存，才标记该地域中已不存在的实例。
	q := a.DB.Model(&Resource{}).Where("account_id = ? AND kind = ? AND region = ?", ac.ID, kind, region)
	if len(ids) > 0 {
		q = q.Where("instance_id NOT IN ?", ids)
	}
	return q.Update("state", "NOT_FOUND").Error
}
