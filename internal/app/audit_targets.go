package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

type AuditTarget struct {
	ResourceID  uint64 `json:"resource_id,omitempty"`
	Name        string `json:"name"`
	InstanceID  string `json:"instance_id,omitempty"`
	AccountID   uint64 `json:"account_id,omitempty"`
	AccountName string `json:"account_name,omitempty"`
	Region      string `json:"region,omitempty"`
	Kind        string `json:"kind"`
}

type AuditTask struct {
	ID        uint64          `json:"id"`
	Status    string          `json:"status"`
	Error     string          `json:"error"`
	RequestID string          `json:"request_id"`
	Results   []RefreshResult `json:"results"`
}

func submittedAudit(action string) bool {
	return action == "accounts.sync" || action == "resources.sync"
}

func auditTargetLabel(targets []AuditTarget) string {
	if len(targets) == 0 {
		return "未记录目标"
	}
	if len(targets) == 1 {
		return targets[0].Name
	}
	unit := "台实例"
	if targets[0].Kind == "account" {
		unit = "个云账号"
	}
	return fmt.Sprintf("%d %s", len(targets), unit)
}

func auditResourceTargets(db *gorm.DB, ids []uint64) ([]AuditTarget, error) {
	var resources []Resource
	if err := db.Select("id, name, instance_id, account_id, kind, region").Where("id IN ?", ids).Find(&resources).Error; err != nil {
		return nil, err
	}
	byID := map[uint64]Resource{}
	accountIDs := []uint64{}
	for _, r := range resources {
		byID[r.ID] = r
		accountIDs = append(accountIDs, r.AccountID)
	}
	var accounts []CloudAccount
	if len(accountIDs) > 0 {
		if err := db.Select("id, name").Where("id IN ?", accountIDs).Find(&accounts).Error; err != nil {
			return nil, err
		}
	}
	accountNames := map[uint64]string{}
	for _, ac := range accounts {
		accountNames[ac.ID] = ac.Name
	}
	targets := make([]AuditTarget, 0, len(ids))
	for _, id := range ids {
		r, exists := byID[id]
		if !exists {
			targets = append(targets, AuditTarget{ResourceID: id, Kind: "resource", Name: fmt.Sprintf("实例记录 #%d（无法解析）", id)})
			continue
		}
		name := r.Name
		if name == "" {
			name = r.InstanceID
		}
		targets = append(targets, AuditTarget{ResourceID: id, Name: name, InstanceID: r.InstanceID, AccountID: r.AccountID, AccountName: accountNames[r.AccountID], Region: r.Region, Kind: r.Kind})
	}
	return targets, nil
}

// 名称快照只包含展示字段；原始目标和请求标识仍单独保存，不写入凭证或完整对象。
func fillAuditTarget(db *gorm.DB, row *Audit) error {
	if row.TargetLabel != "" {
		return nil
	}
	row.TargetDetails = []AuditTarget{}
	target := row.Target
	switch {
	case row.Action == "resources.monitor" && strings.HasPrefix(target, "["):
		var ids []uint64
		if json.Unmarshal([]byte(target), &ids) == nil && len(ids) > 0 && len(ids) <= 100 {
			var err error
			row.TargetDetails, err = auditResourceTargets(db, ids)
			if err != nil {
				return err
			}
			row.TargetLabel = auditTargetLabel(row.TargetDetails)
			return nil
		}
		row.TargetLabel = "所选实例（未记录明细）"
	case strings.HasPrefix(row.Action, "instances.") || row.Action == "resources.read" || row.Action == "monitor.read":
		parts := strings.Split(target, "/")
		if len(parts) == 5 {
			accountID, err := strconv.ParseUint(parts[1], 10, 64)
			if err == nil {
				var r Resource
				err = db.Select("id").Where("account_id = ? AND kind = ? AND region = ? AND instance_id = ?", accountID, parts[2], parts[3], parts[4]).First(&r).Error
				if err == nil {
					row.TargetDetails, err = auditResourceTargets(db, []uint64{r.ID})
					if err != nil {
						return err
					}
					row.TargetLabel = auditTargetLabel(row.TargetDetails)
					return nil
				}
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				row.TargetDetails = []AuditTarget{{Name: parts[4], InstanceID: parts[4], AccountID: accountID, Kind: parts[2], Region: parts[3]}}
				row.TargetLabel = parts[4]
				return nil
			}
		}
		row.TargetLabel = "资源操作"
	case row.Action == "accounts.sync":
		id, err := strconv.ParseUint(target, 10, 64)
		if err == nil {
			var ac CloudAccount
			err = db.Select("id, name").First(&ac, id).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			name := ac.Name
			if name == "" {
				name = fmt.Sprintf("云账号 #%d（无法解析）", id)
			}
			row.TargetLabel = name
			row.TargetDetails = []AuditTarget{{Kind: "account", Name: name, AccountID: id, AccountName: name}}
		}
	case row.Action == "resources.sync":
		row.TargetLabel = "授权云账号范围"
	case row.Action == "settings.save" || row.Action == "settings.manage":
		row.TargetLabel = "系统设置"
	case row.Action == "oauth.link":
		id, err := strconv.ParseUint(target, 10, 64)
		if err == nil {
			var provider OAuthProvider
			err = db.Select("id, name").First(&provider, id).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			row.TargetLabel = provider.Name
		}
		if row.TargetLabel == "" {
			row.TargetLabel = "第三方身份绑定"
		}
	case row.Action == "identities.unlink":
		row.TargetLabel = "第三方身份关联"
	case strings.HasPrefix(row.Action, "auth.") || row.Action == "oauth.login":
		row.TargetLabel = "登录认证"
		if row.Actor != "" && row.Actor != "anonymous" {
			row.TargetLabel = row.Actor
		}
	case row.Action == "request.rejected":
		row.TargetLabel = "平台请求"
	default:
		if target != "" {
			row.TargetLabel = target
		}
	}
	if target == "selected-resources" {
		row.TargetLabel = "所选实例"
	}
	if row.TargetLabel == "" {
		row.TargetLabel = "未记录目标"
	}
	return nil
}

func presentAudits(db *gorm.DB, rows []Audit) error {
	ids := make([]uint64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	var tasks []Task
	if len(ids) > 0 {
		if err := db.Where("audit_id IN ?", ids).Order("id").Find(&tasks).Error; err != nil {
			return err
		}
	}
	byAudit := map[uint64]Task{}
	for _, task := range tasks {
		byAudit[*task.AuditID] = task
	}
	for i := range rows {
		row := &rows[i]
		row.TargetSource = "snapshot"
		if row.TargetLabel == "" {
			if err := fillAuditTarget(db, row); err != nil {
				return err
			}
			row.TargetSource = "current"
		}
		row.DisplayResult, row.DisplayMessage = row.Result, row.Message
		if submittedAudit(row.Action) && slices.Contains([]string{"success", "accepted"}, row.Result) {
			row.DisplayResult = "accepted"
			row.DisplayMessage = "同步任务已提交，执行结果请查看云账号同步日志"
		}
		if task, ok := byAudit[row.ID]; ok {
			row.Task = &AuditTask{ID: task.ID, Status: task.Status, Error: task.Error, RequestID: task.RequestID, Results: task.RefreshResults}
			if row.TargetSource != "snapshot" && len(task.RefreshResults) > 0 {
				for j := range row.TargetDetails {
					for _, result := range task.RefreshResults {
						if result.ResourceID == row.TargetDetails[j].ResourceID {
							if result.Name != "" {
								row.TargetDetails[j].Name = result.Name
							}
							row.TargetDetails[j].InstanceID = result.InstanceID
						}
					}
				}
				row.TargetLabel, row.TargetSource = auditTargetLabel(row.TargetDetails), "task"
			}
			row.DisplayResult = task.Status
			if task.Action == "monitor" {
				counts := map[string]int{}
				for _, result := range task.RefreshResults {
					counts[result.Status]++
				}
				if len(task.RefreshResults) > 0 {
					row.DisplayMessage = fmt.Sprintf("成功 %d 台 · 失败 %d 台 · 跳过 %d 台", counts["succeeded"], counts["failed"], counts["skipped"])
				} else if task.Status == "queued" {
					row.DisplayMessage = "刷新任务已受理，等待执行"
				} else {
					row.DisplayMessage = "查看详情了解刷新任务状态"
				}
			} else if task.Status == "succeeded" {
				row.DisplayMessage = "云端操作已完成"
			} else if task.Status == "failed" {
				row.DisplayMessage = "操作失败，查看详情了解原因"
			}
		}
		if row.DisplayMessage == "" {
			row.DisplayMessage = "—"
		}
	}
	return nil
}
