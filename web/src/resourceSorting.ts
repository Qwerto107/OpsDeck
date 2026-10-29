import type { DataTableBaseColumn, DataTableSortState } from "naive-ui";
import type { Resource, SortRule } from "./types";

export const resourceSortFields = [
  { label: "名称", value: "name" },
  { label: "地域", value: "region" },
  { label: "标签", value: "tag" },
  { label: "CPU 使用率", value: "cpu" },
  { label: "内存使用率", value: "memory" },
  { label: "磁盘使用率", value: "disk" },
];

export function resourceSortLabel(rule: SortRule) {
  return rule.field === "tag"
    ? "标签 " + (rule.key || "")
    : resourceSortFields.find((item) => item.value === rule.field)?.label ||
        rule.field;
}

function columnField(key: string | number): SortRule["field"] | undefined {
  const fields: Record<string, SortRule["field"]> = {
    name: "name",
    account: "region",
    cpu: "cpu",
    memory: "memory",
    disk: "disk",
    metric_cpu: "cpu",
    metric_memory: "memory",
    metric_disk: "disk",
  };
  return fields[String(key)];
}

export function resourceColumnSort(
  key: string | number,
  rules: SortRule[],
): Pick<DataTableBaseColumn<Resource>, "sorter" | "sortOrder"> {
  const field = columnField(key);
  if (!field) return {};
  const index = rules.findIndex((rule) => rule.field === field);
  return {
    sorter: { multiple: index < 0 ? 0 : 8 - index },
    sortOrder: index < 0 ? false : rules[index]!.desc ? "descend" : "ascend",
  };
}

export function updateResourceHeaderSort(
  rules: SortRule[],
  state: DataTableSortState | DataTableSortState[] | null,
): SortRule[] {
  const states = state == null ? [] : Array.isArray(state) ? state : [state];
  // Naive UI 返回当前表头的完整状态，保留不可见列及标签条件，仅调整本次点击的字段。
  const changed = states.find((item) => {
    const field = columnField(item.columnKey);
    if (!field) return false;
    const previous = rules.find((rule) => rule.field === field);
    return item.order === false
      ? !!previous
      : !previous || previous.desc !== (item.order === "descend");
  });
  if (!changed) return rules;
  const field = columnField(changed.columnKey)!;
  const remaining = rules.filter((rule) => rule.field !== field);
  return changed.order === false
    ? remaining
    : [{ field, desc: changed.order === "descend" }, ...remaining];
}
