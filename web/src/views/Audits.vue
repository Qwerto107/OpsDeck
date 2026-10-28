<script setup lang="ts">
import { h, onMounted, ref, watch } from "vue";
import {
  NDataTable,
  NInput,
  NSelect,
  NButton,
  NPagination,
  NAlert,
  NDatePicker,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { Search, RefreshCw } from "lucide-vue-next";
import { api, query } from "../api";
import type { Audit, Page } from "../types";
import StatusBadge from "../components/StatusBadge.vue";
const rows = ref<Audit[]>([]),
  total = ref(0),
  page = ref(1),
  actor = ref(""),
  action = ref(""),
  q = ref(""),
  result = ref<string | null>(null),
  range = ref<[number, number] | null>(null),
  busy = ref(false),
  error = ref("");
let request = 0;
async function load() {
  const seq = ++request;
  busy.value = true;
  error.value = "";
  try {
    const data = await api<Page<Audit>>(
      "/audits" +
        query({
          page: page.value,
          actor: actor.value,
          action: action.value,
          result: result.value,
          q: q.value,
          from: range.value ? new Date(range.value[0]).toISOString() : null,
          to: range.value ? new Date(range.value[1]).toISOString() : null,
        }),
    );
    if (seq !== request) return;
    rows.value = data.items || [];
    total.value = data.total;
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    if (seq === request) busy.value = false;
  }
}
function search() {
  if (page.value !== 1) page.value = 1;
  else void load();
}
const columns: DataTableColumns<Audit> = [
  {
    title: "时间",
    key: "created_at",
    width: 165,
    render: (r) => new Date(r.created_at).toLocaleString(),
  },
  {
    title: "操作人 / 来源",
    key: "actor",
    width: 150,
    render: (r) =>
      h("div", [r.actor, h("div", { class: "subline mono" }, r.source_ip)]),
  },
  { title: "操作", key: "action", width: 170 },
  { title: "目标资源", key: "target", width: 270, ellipsis: { tooltip: true } },
  {
    title: "结果",
    key: "result",
    width: 105,
    render: (r) => h(StatusBadge, { value: r.result }),
  },
  { title: "说明", key: "message", width: 220 },
  {
    title: "请求 ID",
    key: "request_id",
    width: 260,
    render: (r) => h("span", { class: "mono" }, r.request_id),
  },
];
watch(page, load);
onMounted(load);
</script>
<template>
  <div class="panel">
    <form class="toolbar" @submit.prevent="search">
      <n-input
        v-model:value="q"
        class="search"
        placeholder="搜索目标资源或说明"
        clearable
        aria-label="搜索审计日志"
        ><template #prefix><Search :size="15" /></template></n-input
      ><n-input
        v-model:value="actor"
        class="filter"
        placeholder="操作人账号"
        clearable
      /><n-input
        v-model:value="action"
        class="filter"
        placeholder="操作类型"
        clearable
      /><n-select
        v-model:value="result"
        class="filter"
        placeholder="全部结果"
        :options="
          [
            'success',
            'succeeded',
            'failed',
            'denied',
            'accepted',
            'waiting',
            'unknown',
          ].map((value) => ({
            value,
            label: (
              {
                success: '成功',
                succeeded: '已完成',
                failed: '失败',
                denied: '已拒绝',
                accepted: '已受理',
                waiting: '进行中',
                unknown: '待确认',
              } as Record<string, string>
            )[value],
          }))
        "
        clearable
      /><n-date-picker
        v-model:value="range"
        type="datetimerange"
        clearable
        style="max-width: 320px"
      /><n-button type="primary" attr-type="submit">查询</n-button
      ><n-button
        quaternary
        :loading="busy"
        aria-label="刷新审计日志"
        @click="load"
        ><RefreshCw :size="15"
      /></n-button>
    </form>
  </div>
  <n-alert v-if="error" type="error" class="error-banner">{{ error }}</n-alert>
  <div class="panel flush">
    <n-data-table
      :columns="columns"
      :data="rows"
      :loading="busy"
      :bordered="false"
      :scroll-x="1340"
    />
    <div class="table-footer">
      <span class="muted small">共 {{ total }} 条记录 · 敏感操作全程留痕</span
      ><n-pagination
        v-model:page="page"
        :item-count="total"
        :page-size="30"
        size="small"
      />
    </div>
  </div>
</template>
