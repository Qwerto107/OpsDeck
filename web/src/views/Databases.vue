<script setup lang="ts">
import { computed, h, onMounted, onUnmounted, ref, watch } from "vue";
import { RouterLink } from "vue-router";
import {
  NAlert,
  NButton,
  NDataTable,
  NEmpty,
  NInput,
  NPagination,
  NPopover,
  NSelect,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import {
  RefreshCw,
  Search,
  SlidersHorizontal,
  X,
  ArrowUp,
  ArrowDown,
} from "lucide-vue-next";
import { api, query } from "../api";
import { databaseKinds } from "../clouds";
import { useResourceSync } from "../useResourceSync";
import type {
  Account,
  Page,
  Resource,
  ResourceCache,
  SortRule,
} from "../types";
import StatusBadge from "../components/StatusBadge.vue";

const rows = ref<Resource[]>([]),
  accounts = ref<Account[]>([]),
  cache = ref<ResourceCache>(),
  total = ref(0),
  loading = ref(false),
  error = ref("");
const q = ref(""),
  account = ref<number | null>(null),
  kind = ref<string | null>(null),
  region = ref<string | null>(null),
  state = ref<string | null>(null),
  page = ref(1),
  pageSize = ref(20);
const sorts = ref<SortRule[]>([{ field: "name", desc: false }]);
const fields = [
  { label: "名称", value: "name" },
  { label: "地域", value: "region" },
  { label: "标签", value: "tag" },
];
const directions = [
  { label: "升序", value: "asc" },
  { label: "降序", value: "desc" },
];
const regions = computed(() =>
  [
    ...new Set(
      accounts.value
        .filter((a) => account.value === null || a.id === account.value)
        .flatMap((a) => a.regions || []),
    ),
  ].map((value) => ({ label: value, value })),
);
const states = [
  { label: "运行中", value: "RUNNING" },
  { label: "创建中", value: "CREATING" },
  { label: "更新中", value: "UPDATING" },
  { label: "重启中", value: "RESTARTING" },
  { label: "已隔离", value: "ISOLATED" },
  { label: "云端不存在", value: "NOT_FOUND" },
];
let sequence = 0,
  timer: ReturnType<typeof setTimeout>;
async function load() {
  const seq = ++sequence;
  loading.value = true;
  error.value = "";
  try {
    const data = await api<Page<Resource>>(
      "/resources" +
        query({
          category: "database",
          q: q.value,
          account_id: account.value,
          kind: kind.value,
          region: region.value,
          state: state.value,
          page: page.value,
          page_size: pageSize.value,
          sort: JSON.stringify(sorts.value),
        }),
    );
    if (seq !== sequence) return;
    rows.value = data.items || [];
    total.value = data.total;
    cache.value = data.cache;
  } catch (e) {
    if (seq === sequence) error.value = (e as Error).message;
  } finally {
    if (seq === sequence) loading.value = false;
  }
}
const { refresh, syncing, syncStatus, syncError, cacheLabel } = useResourceSync(
  load,
  () => ({ account_id: account.value }),
  cache,
);
function moveSort(index: number, delta: number) {
  const to = index + delta;
  if (to < 0 || to >= sorts.value.length) return;
  const [rule] = sorts.value.splice(index, 1);
  if (rule) sorts.value.splice(to, 0, rule);
}
const columns: DataTableColumns<Resource> = [
  {
    title: "实例",
    key: "name",
    width: 200,
    fixed: "left",
    render: (r) =>
      h("div", [
        h(
          RouterLink,
          { to: "/databases/" + r.id, class: "instance-name" },
          () => r.name,
        ),
        h("div", { class: "subline mono" }, r.instance_id),
      ]),
  },
  {
    title: "引擎 / 版本",
    key: "engine",
    width: 145,
    render: (r) => `${r.database?.engine || "—"} ${r.database?.version || ""}`,
  },
  {
    title: "账号 · 地域",
    key: "account",
    width: 180,
    render: (r) =>
      h("div", [r.account_name, h("div", { class: "subline" }, r.region)]),
  },
  {
    title: "状态",
    key: "state",
    width: 130,
    render: (r) => h(StatusBadge, { value: r.state }),
  },
  {
    title: "规格 / 配置",
    key: "spec",
    width: 220,
    render: (r) =>
      h("div", [
        r.spec || "—",
        h(
          "div",
          { class: "subline" },
          `${r.cpu || "—"} 核 · ${r.database?.memory_gb || "—"} GB 内存 · ${r.disk || "—"} GB 存储`,
        ),
      ]),
  },
  {
    title: "连接地址",
    key: "endpoints",
    width: 270,
    render: (r) =>
      r.database?.endpoints.length
        ? h(
            "div",
            r.database.endpoints.map((ep) =>
              h(
                "div",
                { class: "mono" },
                `${ep.address}${ep.port ? ":" + ep.port : ""}`,
              ),
            ),
          )
        : "暂无连接地址",
  },
  {
    title: "标签",
    key: "tags",
    width: 180,
    render: (r) =>
      r.tags_error ||
      Object.entries(r.tags || {})
        .map(([k, v]) => `${k}: ${v}`)
        .join("；") ||
      "无标签",
  },
  {
    title: "详情",
    key: "detail",
    width: 80,
    fixed: "right",
    render: (r) => h(RouterLink, { to: "/databases/" + r.id }, () => "查看"),
  },
];
watch(
  [q, account, kind, region, state, sorts],
  () => {
    page.value = 1;
    clearTimeout(timer);
    timer = setTimeout(() => void load(), 250);
  },
  { deep: true },
);
watch([page, pageSize], () => {
  clearTimeout(timer);
  timer = setTimeout(() => void load(), 0);
});
onMounted(async () => {
  await load();
  try {
    accounts.value = await api<Account[]>("/accounts");
  } catch (e) {
    error.value = (e as Error).message;
  }
});
onUnmounted(() => {
  sequence++;
  clearTimeout(timer);
});
</script>
<template>
  <div class="row spread wrap" style="margin-bottom: 20px">
    <div>
      <h1 class="page-title">数据库</h1>
      <p class="muted">MySQL 与 PostgreSQL 资源，只读展示</p>
    </div>
    <div class="row wrap">
      <span class="muted small">{{ cacheLabel }}</span
      ><n-button :loading="syncing" @click="refresh"
        ><template #icon><RefreshCw :size="15" /></template>更新</n-button
      >
    </div>
  </div>
  <div class="panel">
    <div class="toolbar">
      <n-input
        v-model:value="q"
        class="search"
        placeholder="搜索实例 ID、名称或连接地址"
        clearable
        aria-label="搜索数据库"
        ><template #prefix><Search :size="15" /></template></n-input
      ><n-select
        v-model:value="account"
        class="filter"
        :options="accounts.map((a) => ({ label: a.name, value: a.id }))"
        placeholder="云账号：全部"
        clearable
        aria-label="数据库云账号筛选"
      /><n-select
        v-model:value="kind"
        class="filter"
        :options="databaseKinds"
        placeholder="引擎：全部"
        clearable
        aria-label="数据库引擎筛选"
      /><n-select
        v-model:value="region"
        class="filter"
        :options="regions"
        placeholder="地域：全部"
        clearable
        filterable
        aria-label="数据库地域筛选"
      /><n-select
        v-model:value="state"
        class="filter"
        :options="states"
        placeholder="状态：全部"
        clearable
        aria-label="数据库状态筛选"
      />
      <n-popover trigger="click" placement="bottom-end"
        ><template #trigger
          ><n-button><SlidersHorizontal :size="15" />排序</n-button></template
        >
        <div style="width: 520px; max-width: 85vw">
          <h3>多字段排序</h3>
          <div
            v-for="(rule, i) in sorts"
            :key="i"
            class="row"
            style="margin: 12px 0"
          >
            <n-select
              v-model:value="rule.field"
              :options="fields"
              aria-label="数据库排序字段"
            /><n-input
              v-if="rule.field === 'tag'"
              v-model:value="rule.key"
              placeholder="标签键"
              aria-label="数据库排序标签键"
            /><n-select
              :value="rule.desc ? 'desc' : 'asc'"
              @update:value="(value) => (rule.desc = value === 'desc')"
              :options="directions"
              aria-label="数据库排序方向"
            /><n-button
              :disabled="i === 0"
              aria-label="提高排序优先级"
              @click="moveSort(i, -1)"
              ><ArrowUp :size="14" /></n-button
            ><n-button
              :disabled="i === sorts.length - 1"
              aria-label="降低排序优先级"
              @click="moveSort(i, 1)"
              ><ArrowDown :size="14" /></n-button
            ><n-button aria-label="移除排序条件" @click="sorts.splice(i, 1)"
              ><X :size="14"
            /></n-button>
          </div>
          <n-button
            :disabled="sorts.length >= 8"
            @click="sorts.push({ field: 'tag', key: '', desc: false })"
            >添加排序条件</n-button
          >
        </div></n-popover
      >
    </div>
  </div>
  <n-alert v-if="syncStatus" type="info" class="error-banner">{{
    syncStatus
  }}</n-alert
  ><n-alert v-if="syncError" type="warning" class="error-banner">{{
    syncError
  }}</n-alert
  ><n-alert v-if="error" type="error" class="error-banner">{{ error }}</n-alert>
  <div class="panel flush">
    <n-data-table
      :columns="columns"
      :data="rows"
      :loading="loading"
      :bordered="false"
      :scroll-x="1500"
      ><template #empty
        ><n-empty description="暂无符合条件的数据库" style="padding: 40px"
          ><template #extra
            >检查筛选条件及账号授权，首次接入后点击更新同步资源。</template
          ></n-empty
        ></template
      ></n-data-table
    >
    <div class="table-footer">
      <span class="muted small">共 {{ total }} 个数据库实例</span
      ><n-pagination
        v-model:page="page"
        v-model:page-size="pageSize"
        :item-count="total"
        :page-sizes="[20, 50, 100]"
        show-size-picker
        size="small"
      />
    </div>
  </div>
</template>
