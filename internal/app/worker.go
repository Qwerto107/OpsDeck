package app

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (a *App) RunWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var pending sync.WaitGroup
	defer pending.Wait()
	monitorDone := make(chan struct{}, monitorConcurrency)
	inFlight := 0
	nextCleanup := time.Time{}
	for {
	drain:
		for {
			select {
			case <-monitorDone:
				inFlight--
			default:
				break drain
			}
		}
		if ctx.Err() != nil {
			return
		}
		if time.Now().After(nextCleanup) {
			a.cleanup()
			nextCleanup = time.Now().Add(time.Hour)
		}
		task, err := a.claimTask(ctx, inFlight < monitorConcurrency)
		if err == nil {
			if task.Action == "monitor" && task.AccountID != nil {
				inFlight++
				pending.Add(1)
				go func() {
					defer pending.Done()
					defer func() { monitorDone <- struct{}{} }()
					a.process(ctx, task)
				}()
			} else {
				// 仅监控任务并行，服务器操作及结果核对继续由主循环处理。
				a.process(ctx, task)
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			slog.Error("worker claim failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-monitorDone:
			inFlight--
		}
	}
}

func (a *App) claimTask(ctx context.Context, allowMonitor bool) (Task, error) {
	var task Task
	err := a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status IN ? AND next_run <= now() AND (lease_until IS NULL OR lease_until < now())", []string{"queued", "submitting", "waiting", "unknown"})
		if !allowMonitor {
			query = query.Where("action <> 'monitor'")
		}
		// 只领取账号当前最早的到期任务，跨 Worker 也不能绕过已领取或更早的任务。
		query = query.Where(`NOT EXISTS (
			SELECT 1 FROM tasks previous
			WHERE previous.account_id = tasks.account_id AND previous.id <> tasks.id
			AND previous.status IN ('queued','submitting','waiting','unknown')
			AND (previous.lease_until > now() OR
				(previous.next_run <= now() AND (previous.next_run,previous.id) < (tasks.next_run,tasks.id)))
		)`)
		if err := query.Order("next_run,id").First(&task).Error; err != nil {
			return err
		}
		lease := time.Now().Add(6 * time.Minute)
		task.LeaseUntil = &lease
		return tx.Model(&task).Update("lease_until", lease).Error
	})
	return task, err
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
		}{{"DELETE FROM audits WHERE created_at < ?", cutoff}, {"DELETE FROM tasks WHERE status IN ('succeeded','failed') AND updated_at < ?", cutoff}, {"DELETE FROM oauth_states WHERE expires_at < ?", time.Now()}, {"DELETE FROM oauth_bindings WHERE expires_at < ?", time.Now()}, {"DELETE FROM login_attempts WHERE expires_at < ?", time.Now()}, {"DELETE FROM sessions WHERE created_at < ?", time.Now().Add(-time.Duration(s.MaxHours) * time.Hour)}} {
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
	if status == "failed" {
		slog.Error("task failed", "task_id", task.ID, "account_id", task.AccountID, "resource_id", task.ResourceID, "action", task.Action, "detail", message, "cloud_request_id", requestID)
	}
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
			if e := tx.Model(&Audit{}).Where("id = ?", *task.AuditID).Updates(map[string]any{"result": result, "message": message}).Error; e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		slog.Error("task state persist failed", "task_id", task.ID, "error_code", "TaskStatePersistFailed")
	}
}
func (a *App) process(parent context.Context, task Task) {
	parent = context.WithValue(parent, cloudTaskContextKey{}, task.ID)
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	if task.Action == "monitor" {
		a.refreshMonitor(ctx, task)
		return
	}
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
	if !isServer(r.Kind) {
		a.finish(task, "failed", "该资源仅支持查看", task.RequestID)
		return
	}
	state, e := a.Cloud.State(ctx, ac, r)
	if e != nil {
		if task.Status == "queued" {
			a.finish(task, "failed", "无法校验云端实例状态；"+syncFailure("状态查询", r.Kind, r.Region, e), task.RequestID)
		} else {
			a.finish(task, "unknown", "无法查询云端结果，将继续核对；"+syncFailure("状态查询", r.Kind, r.Region, e), task.RequestID)
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
				a.finish(task, "failed", ce.Error(), ce.RequestID)
			} else {
				a.finish(task, "unknown", "请求结果不明，将继续核对；不会自动重复提交；"+syncFailure("提交操作", r.Kind, r.Region, err), id)
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
		slog.ErrorContext(ctx, "cloud resource sync failed", "task_id", task.ID, "account_id", ac.ID, "stage", "读取监控设置", "error_code", "SettingsReadFailed")
		a.finish(task, "failed", "无法读取监控设置", "")
		return
	}
	if e = a.DB.Model(&task).Update("status", "submitting").Error; e != nil {
		slog.ErrorContext(ctx, "cloud resource sync failed", "task_id", task.ID, "account_id", ac.ID, "stage", "保存任务状态", "error_code", "TaskStatePersistFailed")
		return
	}
	issues := []string{}
	var issueMu sync.Mutex
	report := func(stage, kind, region string, err error) {
		detail := syncFailure(stage, kind, region, err)
		issueMu.Lock()
		issues = append(issues, strings.Split(detail, "\n")...)
		issueMu.Unlock()
		slog.ErrorContext(ctx, "cloud resource sync failed", "task_id", task.ID, "account_id", ac.ID, "provider", ac.Provider, "site", ac.Site, "stage", stage, "kind", kind, "region", region, "detail", detail)
	}
	discovered := map[string]bool{}
	discoveryFailed := false
	var pending sync.WaitGroup
	slots := make(chan struct{}, 4)
	// 资源类型由云适配层提供；通用调度层不依赖腾讯云产品名称。
	for _, kind := range a.Cloud.Kinds(ac) {
		regions, err := a.Cloud.Regions(ctx, ac, kind)
		if err != nil {
			report("地域发现", kind, "", err)
			discoveryFailed = true
			continue
		}
	regionsLoop:
		for _, region := range regions {
			discovered[region] = true
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				report("等待资源查询", kind, region, ctx.Err())
				discoveryFailed = true
				break regionsLoop
			}
			pending.Add(1)
			go func(region, kind string) {
				defer pending.Done()
				defer func() { <-slots }()
				if err := a.syncRegion(ctx, ac, region, kind, settings.Metrics); err != nil {
					report("资源同步", kind, region, err)
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
		report("保存地域", "all", "", &CloudError{Code: "RegionPersistFailed", Message: "数据库地域记录写入失败"})
	}
	message := ""
	status := "succeeded"
	if len(issues) > 0 {
		sort.Strings(issues)
		message = strings.Join(issues, "\n")
		status = "failed"
	}
	if a.DB.Model(&ac).Updates(map[string]any{"last_sync": time.Now(), "sync_error": message}).Error != nil {
		report("保存同步结果", "all", "", &CloudError{Code: "SyncResultPersistFailed", Message: "数据库同步结果写入失败"})
		message, status = strings.Join(issues, "\n"), "failed"
	}
	a.finish(task, status, message, "")
}

func (a *App) syncRegion(ctx context.Context, ac CloudAccount, region, kind string, enabled []string) error {
	rows, err := a.Cloud.List(ctx, ac, region, kind)
	if err != nil {
		return err
	}
	if isDatabase(kind) {
		enabled = []string{"cpu", "memory", "disk", "connections"}
	}
	metrics := a.Cloud.MonitorBatch(ctx, ac, rows, enabled)
	issues := []error{}
	var previous []Resource
	if a.DB.Where("account_id = ? AND region = ? AND kind = ?", ac.ID, region, kind).Find(&previous).Error != nil {
		return &CloudError{Code: "SnapshotReadFailed", Message: "数据库资源快照读取失败"}
	}
	oldMetrics := map[string]map[string]Metric{}
	for _, r := range previous {
		oldMetrics[r.InstanceID] = r.Metrics
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.InstanceID)
		r.Metrics = metrics[r.InstanceID]
		if r.Metrics == nil {
			r.Metrics = map[string]Metric{}
		}
		for key, metric := range r.Metrics {
			if metric.Status == "error" {
				issues = append(issues, &CloudError{Code: "MonitorQueryFailed", Message: r.InstanceID + " / " + key + "：" + metric.Reason})
			}
		}
		if r.TagsError != "" {
			issues = append(issues, &CloudError{Code: "TagQueryFailed", Message: r.InstanceID + "：" + r.TagsError})
		}
		// 账号同步同样保留失败指标的旧值，避免刷新失败后把已有监控清空。
		for key, old := range oldMetrics[r.InstanceID] {
			current := r.Metrics[key]
			if old.Value != nil && (current.Status == "error" || (current.SampledAt != nil && old.SampledAt != nil && current.SampledAt.Before(*old.SampledAt))) {
				old.Status = "stale"
				if current.Status == "error" {
					old.Reason = current.Reason + "；保留上次采样"
				}
				r.Metrics[key] = old
			}
		}
		if err := a.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "account_id"}, {Name: "kind"}, {Name: "region"}, {Name: "instance_id"}}, DoUpdates: clause.AssignmentColumns([]string{"name", "public_ip", "private_ip", "spec", "cpu", "memory", "disk", "state", "tags", "tags_error", "metrics", "database", "synced_at"})}).Create(&r).Error; err != nil {
			issues = append(issues, &CloudError{Code: "SnapshotPersistFailed", Message: "数据库资源快照写入失败：" + r.InstanceID})
			return errors.Join(issues...)
		}
	}
	// 列表必须完整查询并成功保存，才标记该地域中已不存在的实例。
	q := a.DB.Model(&Resource{}).Where("account_id = ? AND kind = ? AND region = ?", ac.ID, kind, region)
	if len(ids) > 0 {
		q = q.Where("instance_id NOT IN ?", ids)
	}
	if q.Update("state", "NOT_FOUND").Error != nil {
		issues = append(issues, &CloudError{Code: "SnapshotStatePersistFailed", Message: "数据库资源状态写入失败"})
	}
	return errors.Join(issues...)
}
