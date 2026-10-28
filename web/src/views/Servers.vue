<script setup lang="ts">
import { computed, h, onMounted, onUnmounted, ref, watch } from "vue";
import { RouterLink } from "vue-router";
import {
  NButton,
  NInput,
  NSelect,
  NDataTable,
  NPagination,
  NPopover,
  NCheckbox,
  NAlert,
  NEmpty,
  NSkeleton,
  NDropdown,
  useMessage,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import {
  Search,
  SlidersHorizontal,
  Plus,
  RefreshCw,
  ArrowUp,
  ArrowDown,
  X,
  ChevronDown,
  GripVertical,
} from "lucide-vue-next";
import { api, query } from "../api";
import { useResourceSync } from "../useResourceSync";
import { useSession } from "../store";
import type {
  Resource,
  Account,
  Page,
  SortRule,
  ResourceCache,
} from "../types";
import StatusBadge from "../components/StatusBadge.vue";
import MetricValue from "../components/MetricValue.vue";
import ActionModal from "../components/ActionModal.vue";
const session = useSession(),
  message = useMessage(),
  rows = ref<Resource[]>([]),
  cache = ref<ResourceCache>(),
  accounts = ref<Account[]>([]),
  total = ref(0),
  loading = ref(true),
  error = ref(""),
  q = ref(""),
  account = ref<number | null>(null),
  kind = ref<string | null>(null),
  region = ref<string | null>(null),
  state = ref<string | null>(null),
  page = ref(1),
  pageSize = ref(20),
  sorts = ref<SortRule[]>([{ field: "name", desc: false }]),
  visible = ref([
    "kind",
    "tags",
    "ip",
    "spec",
    "account",
    "state",
    "cpu",
    "memory",
    "disk",
    "network",
  ]),
  sortOpen = ref(false),
  prefReady = ref(false),
  saving = ref(false);
const actionShow = ref(false),
  actionResource = ref<Resource | null>(null),
  actionName = ref("");
let debounce: ReturnType<typeof setTimeout>,
  persist: ReturnType<typeof setTimeout>,
  request = 0;
const fields = [
    { label: "名称", value: "name" },
    { label: "地域", value: "region" },
    { label: "标签", value: "tag" },
  ],
  directions = [
    { label: "升序", value: "asc" },
    { label: "降序", value: "desc" },
  ];
const labels: Record<string, string> = {
  start: "开机",
  stop: "关机",
  reboot: "重启",
};
const columnLabels: Record<string, string> = {
  kind: "类型",
  tags: "标签",
  ip: "公网 / 私网 IP",
  spec: "规格与配置",
  account: "账号 · 地域",
  state: "状态",
  cpu: "CPU",
  memory: "内存",
  disk: "磁盘（最高占用）",
  network: "网络 入 / 出",
};
const columnOrder = ref(Object.keys(columnLabels));
const draggedColumn = ref<string | null>(null);
const columnChoices = computed(() =>
  columnOrder.value
    .map((key) => [key, columnLabels[key]!] as [string, string])
    .filter(
      ([key]) =>
        !["cpu", "memory", "disk", "network"].includes(key) ||
        (key === "network"
          ? session.settings.metrics.some((k) => k.startsWith("network_"))
          : session.settings.metrics.includes(key)),
    ),
);
const regionOptions = computed(() =>
  [
    ...new Set(
      accounts.value
        .filter((a) => account.value == null || a.id === account.value)
        .flatMap((a) => a.regions || []),
    ),
  ].map((r) => ({
    label: r,
    value: r,
  })),
);
const statusOptions = [
  "RUNNING",
  "STOPPED",
  "REBOOTING",
  "STARTING",
  "STOPPING",
  "NOT_FOUND",
].map((value) => ({
  value,
  label: (
    {
      RUNNING: "运行中",
      STOPPED: "已关机",
      REBOOTING: "重启中",
      STARTING: "启动中",
      STOPPING: "关机中",
      NOT_FOUND: "云端不存在",
    } as Record<string, string>
  )[value],
}));
function action(r: Resource, key: string) {
  actionResource.value = r;
  actionName.value = key;
  actionShow.value = true;
}
function lines(top: string, bottom: string) {
  return h("div", [h("div", top), h("div", { class: "subline" }, bottom)]);
}
const columns = computed<DataTableColumns<Resource>>(() => {
  const all: DataTableColumns<Resource> = [
    {
      title: "实例",
      key: "name",
      width: 185,
      fixed: "left",
      render: (r) =>
        h("div", [
          h(
            RouterLink,
            { to: "/servers/" + r.id, class: "instance-name" },
            () => r.name,
          ),
          h("span", { class: "mono muted" }, r.instance_id),
        ]),
    },
    {
      title: "类型",
      key: "kind",
      width: 100,
      render: (r) =>
        h(
          "span",
          { class: "resource-tag" },
          r.kind === "cvm" ? "CVM" : "Lighthouse",
        ),
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
      title: "公网 / 私网 IP",
      key: "ip",
      width: 150,
      render: (r) =>
        lines(r.public_ip || "无公网 IP", r.private_ip || "无私网 IP"),
    },
    {
      title: "规格与配置",
      key: "spec",
      width: 150,
      render: (r) => lines(r.spec, `${r.cpu} 核 ${r.memory} GB · ${r.disk} GB`),
    },
    {
      title: "账号 · 地域",
      key: "account",
      width: 150,
      render: (r) => lines(r.account_name, r.region),
    },
    {
      title: "状态",
      key: "state",
      width: 105,
      render: (r) => h(StatusBadge, { value: r.operation_status || r.state }),
    },
    {
      title: "CPU",
      key: "cpu",
      width: 95,
      render: (r) => h(MetricValue, { metric: r.metrics?.cpu, bar: true }),
    },
    {
      title: "内存",
      key: "memory",
      width: 95,
      render: (r) => h(MetricValue, { metric: r.metrics?.memory, bar: true }),
    },
    {
      title: "磁盘（最高占用）",
      key: "disk",
      width: 135,
      render: (r) => h(MetricValue, { metric: r.metrics?.disk, bar: true }),
    },
    {
      title: "网络 入 / 出",
      key: "network",
      width: 135,
      render: (r) =>
        h("div", [
          session.settings.metrics.includes("network_in")
            ? h("div", { class: "row", style: "gap:4px" }, [
                "↓",
                h(MetricValue, { metric: r.metrics?.network_in }),
              ])
            : null,
          session.settings.metrics.includes("network_out")
            ? h("div", { class: "row", style: "gap:4px;margin-top:4px" }, [
                "↑",
                h(MetricValue, { metric: r.metrics?.network_out }),
              ])
            : null,
        ]),
    },
    {
      title: "操作",
      key: "actions",
      width: 165,
      fixed: "right",
      render: (r) =>
        h("div", { class: "row", style: "gap:7px" }, [
          h(
            NButton,
            { size: "small", tag: "a", href: "/servers/" + r.id },
            () => "详情",
          ),
          h(
            NDropdown,
            {
              options: ["start", "stop", "reboot"]
                .filter(
                  (key) =>
                    (r.allowed_actions || []).includes(key) ||
                    (r.allowed_actions || []).includes(
                      key === "stop"
                        ? "force_stop"
                        : key === "reboot"
                          ? "force_reboot"
                          : "",
                    ),
                )
                .map((key) => ({ key, label: labels[key] })),
              onSelect: (key: string) => action(r, key),
            },
            () =>
              h(
                NButton,
                { size: "small", disabled: !r.allowed_actions?.length },
                () => [
                  "操作",
                  h(ChevronDown, { size: 12, style: "margin-left:4px" }),
                ],
              ),
          ),
        ]),
    },
  ];
  // 实例与操作固定两端，其余列遵循用户保存的顺序。
  const byKey = new Map(
    all.filter((c) => "key" in c).map((c) => [String(c.key), c]),
  );
  return [
    byKey.get("name")!,
    ...columnChoices.value
      .filter(([key]) => visible.value.includes(key))
      .map(([key]) => byKey.get(key)!),
    byKey.get("actions")!,
  ];
});
async function load(silent = false) {
  const seq = ++request;
  if (!silent) loading.value = true;
  error.value = "";
  try {
    const data = await api<Page<Resource>>(
      "/resources" +
        query({
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
    if (seq !== request) return;
    rows.value = data.items || [];
    total.value = data.total;
    cache.value = data.cache;
  } catch (e) {
    if (seq === request) error.value = (e as Error).message;
  } finally {
    if (seq === request) loading.value = false;
  }
}
const { refresh, syncing, syncStatus, syncError, cacheLabel } = useResourceSync(
  async () => {
    await load(true);
    accounts.value = await api("/accounts");
  },
  () => ({ account_id: account.value }),
  cache,
);
function filterChanged() {
  page.value = 1;
  clearTimeout(debounce);
  debounce = setTimeout(() => load(), 250);
}
watch([q, account, kind, region, state], filterChanged);
watch([page, pageSize], () => load());
function validSorts() {
  return (
    sorts.value.every((r) => r.field !== "tag" || r.key?.trim()) &&
    new Set(
      sorts.value.map((r) => r.field + (r.field === "tag" ? ":" + r.key : "")),
    ).size === sorts.value.length
  );
}
async function savePrefs() {
  if (!prefReady.value || !validSorts()) return;
  saving.value = true;
  try {
    await session.preferences({
      sorts: sorts.value,
      columns: visible.value,
      column_order: columnOrder.value,
    });
  } catch (e) {
    message.error((e as Error).message);
  } finally {
    saving.value = false;
  }
}
watch(
  sorts,
  () => {
    if (!prefReady.value || !validSorts()) return;
    page.value = 1;
    void load();
    clearTimeout(persist);
    persist = setTimeout(savePrefs, 500);
  },
  { deep: true },
);
watch(
  [visible, columnOrder],
  () => {
    clearTimeout(persist);
    persist = setTimeout(savePrefs, 500);
  },
  { deep: true },
);
function move(i: number, delta: number) {
  const to = i + delta;
  if (to < 0 || to >= sorts.value.length) return;
  const copy = [...sorts.value];
  [copy[i], copy[to]] = [copy[to]!, copy[i]!];
  sorts.value = copy;
}
function moveColumn(key: string, target: string) {
  const from = columnOrder.value.indexOf(key);
  const to = columnOrder.value.indexOf(target);
  if (from < 0 || to < 0 || from === to) return;
  const copy = [...columnOrder.value];
  copy.splice(from, 1);
  copy.splice(to, 0, key);
  columnOrder.value = copy;
}
function dropColumn(target: string) {
  if (draggedColumn.value) moveColumn(draggedColumn.value, target);
  draggedColumn.value = null;
}
function nudgeColumn(key: string, delta: number) {
  const index = columnChoices.value.findIndex(([k]) => k === key);
  const target = columnChoices.value[index + delta];
  if (target) moveColumn(key, target[0]);
}
function resetColumns() {
  columnOrder.value = Object.keys(columnLabels);
  visible.value = columnChoices.value.map(([key]) => key);
}
function toggleColumn(key: string, value: boolean) {
  visible.value = value
    ? [...visible.value, key]
    : visible.value.filter((k) => k !== key);
}
onMounted(async () => {
  const p = session.user?.preferences;
  if (Array.isArray(p?.sorts)) sorts.value = p.sorts as SortRule[];
  if (Array.isArray(p?.columns)) visible.value = p.columns as string[];
  if (Array.isArray(p?.column_order)) {
    // 忽略失效项，并把新增列补到末尾，兼容旧版本偏好。
    columnOrder.value = [
      ...new Set([
        ...p.column_order.filter(
          (key): key is string =>
            typeof key === "string" && key in columnLabels,
        ),
        ...Object.keys(columnLabels),
      ]),
    ];
  }
  try {
    accounts.value = await api("/accounts");
  } catch (e) {
    error.value = (e as Error).message;
  }
  prefReady.value = true;
  await load();
});
onUnmounted(() => {
  clearTimeout(debounce);
  clearTimeout(persist);
  request++;
});
</script>
<template>
  <div class="panel">
    <div class="toolbar">
      <n-input
        v-model:value="q"
        class="search"
        placeholder="搜索实例 ID、名称或 IP"
        clearable
        aria-label="搜索服务器"
        ><template #prefix><Search :size="15" /></template></n-input
      ><n-select
        v-model:value="account"
        class="filter"
        placeholder="云账号：全部"
        :options="accounts.map((a) => ({ label: a.name, value: a.id }))"
        clearable
        aria-label="云账号筛选"
      /><n-select
        v-model:value="kind"
        class="filter"
        placeholder="类型：全部"
        :options="[
          { label: 'CVM', value: 'cvm' },
          { label: 'Lighthouse', value: 'lighthouse' },
        ]"
        clearable
        aria-label="类型筛选"
      /><n-select
        v-model:value="region"
        class="filter"
        placeholder="地域：全部"
        :options="regionOptions"
        filterable
        clearable
        aria-label="地域筛选"
      /><n-select
        v-model:value="state"
        class="filter"
        placeholder="状态：全部"
        :options="statusOptions"
        clearable
        aria-label="状态筛选"
      />
      <div class="row wrap">
        <n-button
          quaternary
          :loading="syncing"
          aria-label="刷新服务器列表"
          @click="refresh"
          ><RefreshCw :size="15" /></n-button
        ><span class="muted small">{{ cacheLabel }}</span
        ><span v-if="syncStatus" class="muted small" role="status">{{
          syncStatus
        }}</span>
      </div>
      <n-popover trigger="click" placement="bottom-end"
        ><template #trigger
          ><n-button class="push"
            ><SlidersHorizontal
              :size="14"
              style="margin-right: 6px"
            />列设置</n-button
          ></template
        >
        <div class="columns-panel">
          <h3>列设置</h3>
          <p class="hint">
            拖动调整顺序，也可聚焦拖动柄后按上下键。仅展示已开启的指标。
          </p>
          <label><n-checkbox checked disabled>实例（固定）</n-checkbox></label>
          <div
            v-for="[key, label] in columnChoices"
            :key="key"
            class="column-option"
            :class="{ dragging: draggedColumn === key }"
            @dragover.prevent
            @drop.prevent="dropColumn(key)"
          >
            <button
              type="button"
              class="column-drag-handle"
              draggable="true"
              :aria-label="'调整' + label + '列顺序'"
              @dragstart="draggedColumn = key"
              @dragend="draggedColumn = null"
              @keydown.up.prevent="nudgeColumn(key, -1)"
              @keydown.down.prevent="nudgeColumn(key, 1)"
            >
              <GripVertical :size="15" />
            </button>
            <n-checkbox
              :checked="visible.includes(key)"
              @update:checked="(v) => toggleColumn(key, v)"
              >{{ label }}</n-checkbox
            >
          </div>
          <label><n-checkbox checked disabled>操作（固定）</n-checkbox></label
          ><n-button text type="primary" @click="resetColumns"
            >恢复默认</n-button
          ><span class="muted small" style="float: right">自动保存</span>
        </div></n-popover
      >
    </div>
    <div class="sortbar">
      <span class="muted small">排序</span
      ><span v-for="(r, i) in sorts" :key="i" class="sort-chip"
        ><small>{{ i + 1 }}</small
        >{{
          r.field === "tag"
            ? "标签 " + r.key
            : r.field === "region"
              ? "地域"
              : "名称"
        }}<component :is="r.desc ? ArrowDown : ArrowUp" :size="12" /></span
      ><n-popover
        v-model:show="sortOpen"
        trigger="click"
        placement="bottom-start"
        ><template #trigger
          ><n-button size="small" quaternary
            ><Plus :size="14" />调整排序</n-button
          ></template
        >
        <div class="sort-editor">
          <h3>多字段排序</h3>
          <p class="hint" style="margin-bottom: 16px">
            按优先级依次比较，缺失值始终排在末尾。
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
                @click="move(i, -1)"
                ><ArrowUp :size="13" /></n-button
              ><n-button
                text
                :disabled="i === sorts.length - 1"
                aria-label="降低排序优先级"
                @click="move(i, 1)"
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
      ><span class="muted small">{{
        saving ? "保存中…" : "按账号自动记忆排序"
      }}</span
      ><span class="grow" /><span class="muted small"
        >云监控快照 · 缓存 3 分钟 · 手动更新</span
      >
    </div>
  </div>
  <n-alert v-if="syncError" type="warning" class="error-banner"
    >{{ syncError }}
    <n-button text @click="refresh">重试更新</n-button></n-alert
  >
  <n-alert
    v-if="error"
    type="error"
    class="error-banner"
    closable
    @close="error = ''"
    >{{ error }}
    <n-button text type="primary" @click="load()">重试</n-button></n-alert
  >
  <div class="panel flush">
    <div v-if="loading && !rows.length" style="padding: 24px">
      <n-skeleton
        v-for="i in 5"
        :key="i"
        height="42px"
        style="margin-bottom: 15px"
      />
    </div>
    <n-data-table
      v-else
      :columns="columns"
      :data="rows"
      :row-key="(r) => r.id"
      :loading="loading"
      :bordered="false"
      :scroll-x="columns.reduce((s, c) => s + Number(c.width || 100), 0)"
      ><template #empty
        ><n-empty description="暂无符合条件的服务器" style="padding: 40px 0"
          ><template #extra
            ><div class="empty-text muted">
              检查筛选条件，或联系管理员接入云账号并分配资源权限。首次接入后需要等待
              Worker 完成同步。
            </div>
            <n-button
              v-if="session.can('accounts.manage')"
              tag="a"
              href="/accounts"
              style="margin-top: 16px"
              >接入云账号</n-button
            ></template
          ></n-empty
        ></template
      ></n-data-table
    >
    <div class="table-footer">
      <span class="muted small">共 {{ total }} 台，仅显示你有权访问的实例</span
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
  <action-modal
    v-model:show="actionShow"
    :resource="actionResource"
    :action="actionName"
    @done="load(true)"
  />
</template>

<style scoped>
.column-option {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 0;
  border-radius: 4px;
}
.column-option.dragging {
  background: #eef2ff;
}
.column-drag-handle {
  display: inline-flex;
  align-items: center;
  padding: 3px;
  color: #667085;
  background: transparent;
  border: 0;
  cursor: grab;
}
.column-drag-handle:active {
  cursor: grabbing;
}
.columns-panel .hint {
  max-width: 240px;
}
</style>
