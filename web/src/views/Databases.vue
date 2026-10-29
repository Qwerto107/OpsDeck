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
  useMessage,
} from "naive-ui";
import type {
  DataTableBaseColumn,
  DataTableColumns,
  DataTableSortState,
} from "naive-ui";
import { Search, Plus, X, ArrowUp, ArrowDown } from "lucide-vue-next";
import { api, query } from "../api";
import {
  resourceSortFields,
  resourceSortLabel,
  resourceColumnSort,
  updateResourceHeaderSort,
} from "../resourceSorting";
import { databaseKinds, databaseMetrics } from "../clouds";
import { useResourceSync } from "../useResourceSync";
import { useSession } from "../store";
import type {
  Account,
  Page,
  Resource,
  ResourceCache,
  SortRule,
} from "../types";
import StatusBadge from "../components/StatusBadge.vue";
import ResourceSyncControl from "../components/ResourceSyncControl.vue";
import MetricValue from "../components/MetricValue.vue";
import ResourceColumns from "../components/ResourceColumns.vue";

const message = useMessage(),
  session = useSession();
const rows = ref<Resource[]>([]),
  accounts = ref<Account[]>([]),
  cache = ref<ResourceCache>(),
  total = ref(0),
  loading = ref(false),
  error = ref("");
const q = ref(""),
  account = ref<number[]>([]),
  kind = ref<string[]>([]),
  region = ref<string[]>([]),
  state = ref<string[]>([]),
  page = ref(1),
  pageSize = ref(20);
const sorts = ref<SortRule[]>([{ field: "name", desc: false }]);
const fields = resourceSortFields;
const directions = [
  { label: "升序", value: "asc" },
  { label: "降序", value: "desc" },
];
const regions = computed(() =>
  [
    ...new Set(
      accounts.value
        .filter((a) => !account.value.length || account.value.includes(a.id))
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
  timer: ReturnType<typeof setTimeout>,
  persist: ReturnType<typeof setTimeout>;
function updateHeaderSort(
  state: DataTableSortState | DataTableSortState[] | null,
) {
  const next = updateResourceHeaderSort(sorts.value, state);
  if (next.length > 8) {
    message.warning("最多设置 8 个排序条件，请先移除一个条件");
    return;
  }
  sorts.value = next;
}
function validSorts() {
  return (
    sorts.value.every((r) => r.field !== "tag" || r.key?.trim()) &&
    new Set(
      sorts.value.map((r) => r.field + (r.field === "tag" ? ":" + r.key : "")),
    ).size === sorts.value.length
  );
}
async function load() {
  if (!validSorts()) return;
  const seq = ++sequence;
  loading.value = true;
  error.value = "";
  try {
    const data = await api<Page<Resource>>(
      "/resources" +
        query({
          category: "database",
          q: q.value,
          account_id: account.value.join(","),
          kind: kind.value.join(","),
          region: region.value.join(","),
          state: state.value.join(","),
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
const { refresh, retryFailed, syncState } = useResourceSync(
  load,
  () => ({
    resource_ids: rows.value
      .filter((r) => r.can_refresh_monitor)
      .map((r) => r.id),
  }),
  cache,
);
function moveSort(index: number, delta: number) {
  const to = index + delta;
  if (to < 0 || to >= sorts.value.length) return;
  const [rule] = sorts.value.splice(index, 1);
  if (rule) sorts.value.splice(to, 0, rule);
}
const baseColumns: DataTableBaseColumn<Resource>[] = [
  {
    title: "实例",
    key: "name",
    width: 185,
    fixed: "left",
    render: (r) =>
      h("div", [
        h(
          RouterLink,
          { to: "/databases/" + r.id, class: "instance-name" },
          () => r.name,
        ),
        h("span", { class: "mono muted" }, r.instance_id),
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
    width: 150,
    render: (r) =>
      h("div", [r.account_name, h("div", { class: "subline" }, r.region)]),
  },
  {
    title: "状态",
    key: "state",
    width: 105,
    render: (r) => h(StatusBadge, { value: r.state }),
  },
  {
    title: "规格与配置",
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
  ...databaseMetrics.map(({ key, label }) => ({
    title: label,
    key: "metric_" + key,
    width: key === "disk" ? 145 : 110,
    render: (resource: Resource) =>
      h(MetricValue, {
        metric: resource.metrics?.[key],
        bar: key !== "connections",
      }),
  })),
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
    width: 160,
    render: (r) =>
      r.tags_error
        ? h("span", { class: "metric-na" }, "标签获取失败")
        : Object.keys(r.tags || {}).length
          ? h(
              "div",
              { class: "tag-list" },
              Object.entries(r.tags).map(([k, v]) =>
                h("span", { class: "resource-tag" }, `${k}: ${v}`),
              ),
            )
          : h("span", { class: "muted" }, "无标签"),
  },
  {
    title: "详情",
    key: "detail",
    width: 80,
    fixed: "right",
    render: (r) => h(RouterLink, { to: "/databases/" + r.id }, () => "查看"),
  },
];
const configurableColumns = baseColumns.filter(
  (column) => column.key !== "name" && column.key !== "detail",
);
const columnLabels = Object.fromEntries(
  configurableColumns.map((column) => [
    String(column.key),
    String(column.title),
  ]),
);
const defaultOrder = Object.keys(columnLabels),
  columnOrder = ref([...defaultOrder]),
  visible = ref([...defaultOrder]),
  prefReady = ref(false);
const columnChoices = computed(() =>
  columnOrder.value.map((key) => [key, columnLabels[key]!] as [string, string]),
);
const columns = computed<DataTableColumns<Resource>>(() => {
  const byKey = new Map(
    baseColumns.map((column) => [String(column.key), column]),
  );
  return [
    byKey.get("name")!,
    ...columnChoices.value
      .filter(([key]) => visible.value.includes(key))
      .map(([key]) => byKey.get(key)!),
    byKey.get("detail")!,
  ].map((column) => ({
    ...column,
    ...resourceColumnSort(
      String("key" in column ? column.key : ""),
      sorts.value,
    ),
  }));
});
async function saveColumns() {
  try {
    await session.preferences({
      database_columns: visible.value,
      database_column_order: columnOrder.value,
    });
  } catch (e) {
    message.error((e as Error).message);
  }
}
watch(
  [visible, columnOrder],
  () => {
    if (!prefReady.value) return;
    clearTimeout(persist);
    persist = setTimeout(saveColumns, 500);
  },
  { deep: true },
);
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
  const p = session.user?.preferences;
  if (Array.isArray(p?.database_columns))
    visible.value = p.database_columns as string[];
  if (Array.isArray(p?.database_column_order)) {
    // 忽略失效项，并补齐新增列；数据库与服务器分别保存列偏好。
    columnOrder.value = [
      ...new Set([
        ...p.database_column_order.filter(
          (key): key is string =>
            typeof key === "string" && Object.hasOwn(columnLabels, key),
        ),
        ...defaultOrder,
      ]),
    ];
  }
  prefReady.value = true;
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
  clearTimeout(persist);
});
</script>
<template>
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
        multiple
        max-tag-count="responsive"
        class="filter"
        :options="accounts.map((a) => ({ label: a.name, value: a.id }))"
        placeholder="云账号：全部"
        clearable
        aria-label="数据库云账号筛选"
      /><n-select
        v-model:value="kind"
        multiple
        max-tag-count="responsive"
        class="filter"
        :options="databaseKinds"
        placeholder="引擎：全部"
        clearable
        aria-label="数据库引擎筛选"
      /><n-select
        v-model:value="region"
        multiple
        max-tag-count="responsive"
        class="filter"
        :options="regions"
        placeholder="地域：全部"
        clearable
        filterable
        aria-label="数据库地域筛选"
      /><n-select
        v-model:value="state"
        multiple
        max-tag-count="responsive"
        class="filter"
        :options="states"
        placeholder="状态：全部"
        clearable
        aria-label="数据库状态筛选"
      />
      <ResourceColumns
        v-model:visible="visible"
        v-model:order="columnOrder"
        :choices="columnChoices"
        :default-order="defaultOrder"
        last-label="详情"
      />
    </div>
    <div class="sortbar">
      <span class="muted small">排序</span
      ><span v-for="(r, i) in sorts" :key="i" class="sort-chip"
        ><small>{{ i + 1 }}</small
        >{{ resourceSortLabel(r)
        }}<component :is="r.desc ? ArrowDown : ArrowUp" :size="12" /></span
      ><n-popover trigger="click" placement="bottom-start"
        ><template #trigger
          ><n-button size="small" quaternary
            ><Plus :size="14" />调整排序</n-button
          ></template
        >
        <div class="sort-editor">
          <h3>多字段排序</h3>
          <p class="hint" style="margin-bottom: 16px">
            按优先级依次比较，缺失值始终排在末尾。点击表头可切换排序并将该字段置于首位。
          </p>
          <div v-for="(r, i) in sorts" :key="i" class="sort-row">
            <span class="muted">{{ i + 1 }}</span
            ><n-select
              v-model:value="r.field"
              :options="fields"
              aria-label="排序字段"
            /><n-input
              v-if="r.field === 'tag'"
              v-model:value="r.key"
              placeholder="标签键，如 environment"
              aria-label="标签键"
            /><span v-else /><n-select
              :value="r.desc ? 'desc' : 'asc'"
              @update:value="(v) => (r.desc = v === 'desc')"
              :options="directions"
              aria-label="排序方向"
            />
            <div class="row" style="gap: 2px">
              <n-button
                text
                :disabled="i === 0"
                aria-label="提高排序优先级"
                @click="moveSort(i, -1)"
                ><ArrowUp :size="13" /></n-button
              ><n-button
                text
                :disabled="i === sorts.length - 1"
                aria-label="降低排序优先级"
                @click="moveSort(i, 1)"
                ><ArrowDown :size="13" /></n-button
              ><n-button
                text
                aria-label="移除排序条件"
                @click="sorts.splice(i, 1)"
                ><X :size="13"
              /></n-button>
            </div>
          </div>
          <n-alert
            v-if="!validSorts()"
            type="warning"
            style="margin-bottom: 10px"
            >请填写标签键，且不要重复选择同一字段或标签键。</n-alert
          ><n-button
            size="small"
            :disabled="sorts.length >= 8"
            @click="sorts.push({ field: 'tag', key: '', desc: false })"
            >添加排序条件</n-button
          >
        </div></n-popover
      ><span class="grow" />
      <ResourceSyncControl
        :state="syncState"
        scope-label="仅刷新当前页有监控权限的实例"
        @refresh="refresh"
      />
    </div>
  </div>
  <ResourceSyncControl
    mode="notice"
    :state="syncState"
    @refresh="refresh"
    @retry="retryFailed"
  />
  <n-alert v-if="error" type="error" class="error-banner">{{ error }}</n-alert>
  <div class="panel flush">
    <n-data-table
      remote
      @update:sorter="updateHeaderSort"
      :columns="columns"
      :data="rows"
      :loading="loading"
      :bordered="false"
      :row-key="(r) => r.id"
      :scroll-x="
        columns.reduce((sum, column) => sum + Number(column.width || 100), 0)
      "
      ><template #empty
        ><n-empty description="暂无符合条件的数据库" style="padding: 40px"
          ><template #extra
            >检查筛选条件及账号授权，首次接入后点击更新同步资源。</template
          ></n-empty
        ></template
      ></n-data-table
    >
    <div class="table-footer">
      <span class="muted small"
        >共 {{ total }} 个数据库实例，仅显示你有权访问的资源</span
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
