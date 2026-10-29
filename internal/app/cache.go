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
}

func (a *App) monitorSample(cache *resourceCache, rows []Resource) error {
	settings, err := a.settings()
	if err != nil {
		return err
	}
	cache.StaleAfterSeconds = settings.StaleMinutes * 60
	// 使用当前可见指标的实际采样时间，不能把任务结束时间当成云监控采样时间。
	for _, r := range rows {
		if r.State != "RUNNING" {
			continue
		}
		for _, metric := range r.Metrics {
			if metric.Value != nil && metric.SampledAt != nil && (cache.SampledAt == nil || metric.SampledAt.Before(*cache.SampledAt)) {
				sampled := *metric.SampledAt
				cache.SampledAt = &sampled
			}
		}
	}
	return nil
}

// 过期快照继续可读；只有用户明确刷新才回源，避免重载页面触发云请求。
func (a *App) resourceCache(u User, accounts []CloudAccount, selected ...uint64) (resourceCache, error) {
	cache := resourceCache{TTLSeconds: 180}
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
