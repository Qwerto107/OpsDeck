package app

import (
	"cmp"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type SortRule struct {
	Field string `json:"field"`
	Key   string `json:"key,omitempty"`
	Desc  bool   `json:"desc"`
}

func ParseSort(raw string) ([]SortRule, error) {
	rules := []SortRule{{Field: "name"}}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &rules); err != nil {
			return nil, err
		}
	}
	if len(rules) > 8 {
		return nil, fmt.Errorf("最多设置 8 个排序条件")
	}
	seen := map[string]bool{}
	for _, r := range rules {
		if r.Field != "name" && r.Field != "region" && r.Field != "tag" && r.Field != "cpu" && r.Field != "memory" && r.Field != "disk" {
			return nil, fmt.Errorf("无效排序字段")
		}
		key := r.Field
		if r.Field == "tag" {
			if strings.TrimSpace(r.Key) == "" {
				return nil, fmt.Errorf("请选择标签键")
			}
			key += ":" + r.Key
		}
		if seen[key] {
			return nil, fmt.Errorf("排序字段不能重复")
		}
		seen[key] = true
	}
	return rules, nil
}
func SortResources(rows []Resource, rules []SortRule) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		for _, r := range rules {
			var x, y string
			var comparison int
			switch r.Field {
			case "name":
				x, y = a.Name, b.Name
			case "region":
				x, y = a.Region, b.Region
			case "tag":
				x, y = a.Tags[r.Key], b.Tags[r.Key]
			case "cpu", "memory", "disk":
				// 使用已授权、可展示的监控使用率，不能改用实例配置或泄露被隐藏的指标。
				left, right := a.Metrics[r.Field].Value, b.Metrics[r.Field].Value
				if left == nil && right != nil {
					return false
				}
				if right == nil && left != nil {
					return true
				}
				if left != nil && right != nil {
					comparison = cmp.Compare(*left, *right)
				}
			}
			// 空值始终置后，不能随降序反转。
			if x == "" && y != "" {
				return false
			}
			if y == "" && x != "" {
				return true
			}
			if comparison == 0 {
				comparison = strings.Compare(x, y)
			}
			if comparison != 0 {
				if r.Desc {
					return comparison > 0
				}
				return comparison < 0
			}
		}
		if c := cmp.Compare(a.AccountID, b.AccountID); c != 0 {
			return c < 0
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Region != b.Region {
			return a.Region < b.Region
		}
		return a.InstanceID < b.InstanceID
	})
}
