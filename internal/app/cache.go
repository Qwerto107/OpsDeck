package app

import (
	"slices"
	"time"
)

type resourceCache struct {
	UpdatedAt         *time.Time `json:"updated_at"`
	TTLSeconds        int        `json:"ttl_seconds"`
	SampledAt         *time.Time `json:"sampled_at"`
	StaleAfterSeconds int        `json:"stale_after_seconds"`
	MonitorUpdatedAt  *time.Time `json:"monitor_updated_at"`
	AutoRefresh       bool       `json:"auto_refresh"`
}

func (a *App) monitorSample(cache *resourceCache, rows []Resource) error {
	settings, err := a.settings()
	if err != nil {
		return err
	}
	cache.StaleAfterSeconds = settings.StaleMinutes * 60
	cache.TTLSeconds = settings.StaleMinutes * 60
	eligible, missingRefresh := false, false
	// 使用当前可见指标的实际采样时间，不能把任务结束时间当成云监控采样时间。
	for _, r := range rows {
		if r.State != "RUNNING" {
			continue
		}
		if r.CanRefreshMonitor && (isDatabase(r.Kind) || len(settings.Metrics) > 0) {
			eligible = true
			if r.MonitorRefreshedAt == nil {
				missingRefresh = true
			} else if cache.MonitorUpdatedAt == nil || r.MonitorRefreshedAt.Before(*cache.MonitorUpdatedAt) {
				cache.MonitorUpdatedAt = r.MonitorRefreshedAt
			}
		}
		for _, metric := range r.Metrics {
			if metric.Value != nil && metric.SampledAt != nil && (cache.SampledAt == nil || metric.SampledAt.Before(*cache.SampledAt)) {
				sampled := *metric.SampledAt
				cache.SampledAt = &sampled
			}
		}
	}
	if missingRefresh {
		cache.MonitorUpdatedAt = nil
	}
	cache.AutoRefresh = settings.AutoRefresh && eligible
	return nil
}

// GET 始终只读快照；页面按开关和有效期另行提交监控刷新任务。
func (a *App) resourceCache(u User, accounts []CloudAccount, selected ...uint64) (resourceCache, error) {
	cache := resourceCache{TTLSeconds: 600}
	ids := []uint64{}
	for _, ac := range accounts {
		if len(selected) == 0 && !ac.Enabled {
			continue
		}
		if (len(selected) == 0 || slices.Contains(selected, ac.ID)) && canRefreshAccount(u, ac.ID) {
			ids = append(ids, ac.ID)
		}
	}
	if len(ids) == 0 {
		return cache, nil
	}
	var stamps []struct {
		AccountID uint64
		UpdatedAt time.Time
	}
	err := a.DB.Model(&Task{}).Select("account_id, MAX(updated_at) AS updated_at").Where("account_id IN ? AND action = 'sync' AND status = 'succeeded'", ids).Group("account_id").Scan(&stamps).Error
	if err != nil {
		return cache, err
	}
	// 多账号显示最早的成功更新时间；任一账号尚未成功同步则不宣称整体缓存有效。
	if len(stamps) != len(ids) {
		return cache, nil
	}
	for _, stamp := range stamps {
		if cache.UpdatedAt == nil || stamp.UpdatedAt.Before(*cache.UpdatedAt) {
			t := stamp.UpdatedAt
			cache.UpdatedAt = &t
		}
	}
	return cache, nil
}
