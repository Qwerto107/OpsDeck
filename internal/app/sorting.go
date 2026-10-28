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
		if r.Field != "name" && r.Field != "region" && r.Field != "tag" {
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
			switch r.Field {
			case "name":
				x, y = a.Name, b.Name
			case "region":
				x, y = a.Region, b.Region
			case "tag":
				x, y = a.Tags[r.Key], b.Tags[r.Key]
			}
			// 空值始终置后，不能随降序反转。
			if x == "" && y != "" {
				return false
			}
			if y == "" && x != "" {
				return true
			}
			if c := strings.Compare(x, y); c != 0 {
				if r.Desc {
					return c > 0
				}
				return c < 0
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
