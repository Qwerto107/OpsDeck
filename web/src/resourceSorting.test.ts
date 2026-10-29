import { describe, expect, it } from "vitest";
import type { DataTableSortState } from "naive-ui";
import type { SortRule } from "./types";
import {
  resourceColumnSort,
  updateResourceHeaderSort,
} from "./resourceSorting";

const state = (
  columnKey: string,
  order: DataTableSortState["order"],
): DataTableSortState => ({ columnKey, order, sorter: { multiple: 1 } });

describe("资源表头排序", () => {
  it("将点击指标置于首位，并保留标签和其他排序条件", () => {
    const rules: SortRule[] = [
      { field: "name", desc: false },
      { field: "tag", key: "env", desc: true },
    ];
    const next = updateResourceHeaderSort(rules, [
      state("name", "ascend"),
      state("metric_cpu", "ascend"),
    ]);
    expect(next).toEqual([{ field: "cpu", desc: false }, ...rules]);
    expect(resourceColumnSort("metric_cpu", next)).toMatchObject({
      sortOrder: "ascend",
      sorter: { multiple: 8 },
    });
    expect(
      updateResourceHeaderSort(next, [
        state("metric_cpu", "descend"),
        state("name", "ascend"),
      ])[0],
    ).toEqual({ field: "cpu", desc: true });
  });

  it("取消表头排序保留隐藏指标和标签，不触发当前页的本地比较", () => {
    const rules: SortRule[] = [
      { field: "memory", desc: true },
      { field: "cpu", desc: false },
      { field: "tag", key: "env", desc: false },
    ];
    expect(updateResourceHeaderSort(rules, [state("cpu", false)])).toEqual([
      rules[0],
      rules[2],
    ]);
    expect(resourceColumnSort("cpu", rules).sorter).not.toHaveProperty(
      "compare",
    );
    expect(resourceColumnSort("endpoints", rules)).toEqual({});
  });
});
