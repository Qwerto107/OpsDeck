<script setup lang="ts">
import { computed, h, onMounted, onUnmounted, ref, watch } from "vue";
import {
  NDataTable,
  NInput,
  NSelect,
  NButton,
  NPagination,
  NAlert,
  NDatePicker,
  NDrawer,
  NDrawerContent,
  NEmpty,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { Search, RefreshCw } from "lucide-vue-next";
import { api, query } from "../api";
import type { Audit, AuditTarget, Page } from "../types";
import { kindLabel } from "../clouds";
import StatusBadge from "../components/StatusBadge.vue";
const rows = ref<Audit[]>([]),
  total = ref(0),
  page = ref(1),
  actor = ref<string[]>([]),
  action = ref<string[]>([]),
  q = ref(""),
  result = ref<string[]>([]),
  range = ref<[number, number] | null>(null),
  busy = ref(false),
  error = ref("");
const selected = ref<Audit | null>(null),
  detailsOpen = ref(false);
const resultLabels: Record<string, string> = {
  success: "成功",
  succeeded: "执行完成",
  failed: "失败",
  denied: "已拒绝",
  accepted: "已受理",
  queued: "排队中",
  submitting: "执行中",
  waiting: "执行中",
  unknown: "待确认",
  skipped: "已跳过",
};
function targetContext(row: Audit) {
  const targets = row.target_details || [];
  const accounts = [
    ...new Set(targets.map((target) => target.account_name).filter(Boolean)),
  ];
  const regions = [
    ...new Set(targets.map((target) => target.region).filter(Boolean)),
  ];
  return [
    ...accounts,
    regions.length === 1
      ? regions[0]
      : regions.length > 1
        ? `${regions.length} 个地域`
        : "",
  ]
    .filter(Boolean)
    .join(" · ");
}
function showDetails(row: Audit) {
  selected.value = row;
  detailsOpen.value = true;
}
const detailTargets = computed(() =>
  (selected.value?.target_details || []).map((target) => ({
    ...target,
    result: selected.value?.task?.results?.find(
      (result) => result.resource_id === target.resource_id,
    ),
  })),
);
function targetKind(target: AuditTarget) {
  const labels: Record<string, string> = {
    account: "云账号",
    identity: "第三方身份关联",
    resource: "实例",
  };
  return labels[target.kind] || kindLabel(target.kind);
}
let request = 0;
const actionLabels: Record<string, string> = {
  "auth.login": "账号登录",
  "auth.logout": "退出登录",
  "auth.password": "修改密码",
  "oauth.login": "第三方登录",
  "oauth.link": "绑定第三方身份",
  "oauth.provider.save": "保存身份提供方",
  "identities.unlink": "解除第三方身份关联",
  "users.save": "保存用户",
  "users.delete": "删除用户",
  "users.reset_password": "重置用户密码",
  "roles.save": "保存角色",
  "roles.delete": "删除角色",
  "accounts.save": "保存云账号",
  "accounts.delete": "删除云账号",
  "accounts.sync": "提交云账号同步",
  "resources.sync": "提交资源同步",
  "resources.monitor": "刷新实例监控",
  "resources.read": "查看资源",
  "monitor.read": "查看监控",
  "instances.start": "实例开机",
  "instances.stop": "实例关机",
  "instances.reboot": "重启实例",
  "instances.force_stop": "强制关机",
  "instances.force_reboot": "强制重启实例",
  "users.manage": "管理用户",
  "roles.manage": "管理角色",
  "accounts.manage": "管理云账号",
  "audit.read": "查看审计日志",
  "settings.manage": "管理系统设置",
  "settings.save": "保存系统设置",
  "request.rejected": "请求被拒绝",
};
async function load() {
  const seq = ++request;
  busy.value = true;
  error.value = "";
  try {
    const data = await api<Page<Audit>>(
      "/audits" +
        query({
          page: page.value,
          actor: actor.value.join(","),
          action: action.value.join(","),
          result: result.value.join(","),
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
    render: (r) =>
      new Date(r.created_at).toLocaleString("zh-CN", { hour12: false }),
  },
  {
    title: "操作人 / 来源",
    key: "actor",
    width: 150,
    render: (r) =>
      h("div", [
        r.actor === "anonymous" ? "未登录用户" : r.actor || "未记录操作人",
        h("div", { class: "subline mono" }, r.source_ip || "未记录来源"),
      ]),
  },
  {
    title: "操作",
    key: "action",
    width: 150,
    render: (r) => actionLabels[r.action] || "其他操作",
  },
  {
    title: "操作目标",
    key: "target",
    width: 270,
    render: (r) =>
      h("div", [
        h(
          "div",
          { class: "audit-target-name", title: r.target_label },
          r.target_label || "未记录目标",
        ),
        h(
          "div",
          { class: "subline", title: targetContext(r) },
          targetContext(r),
        ),
      ]),
  },
  {
    title: "结果",
    key: "result",
    width: 105,
    render: (r) =>
      h(StatusBadge, {
        value: r.display_result || r.result,
        label: resultLabels[r.display_result || r.result] || "未知状态",
      }),
  },
  {
    title: "说明",
    key: "message",
    width: 240,
    ellipsis: { tooltip: true },
    render: (r) => r.display_message || r.message || "—",
  },
  {
    title: "详情",
    key: "details",
    width: 80,
    fixed: "right",
    render: (r) =>
      h(
        NButton,
        {
          size: "small",
          text: true,
          type: "primary",
          onClick: () => showDetails(r),
        },
        () => "查看详情",
      ),
  },
];
watch(page, load);
onMounted(load);
onUnmounted(() => {
  request++;
});
</script>
<template>
  <div class="panel">
    <form class="toolbar" @submit.prevent="search">
      <n-input
        v-model:value="q"
        class="search"
        placeholder="搜索目标名称、实例 ID 或说明"
        clearable
        aria-label="搜索审计日志"
        ><template #prefix><Search :size="15" /></template></n-input
      ><n-select
        v-model:value="actor"
        multiple
        tag
        filterable
        max-tag-count="responsive"
        :options="[]"
        class="filter"
        placeholder="操作人账号"
        aria-label="审计操作人筛选"
        clearable
      /><n-select
        v-model:value="action"
        multiple
        max-tag-count="responsive"
        class="filter"
        placeholder="操作类型"
        aria-label="审计操作类型筛选"
        :options="
          Object.entries(actionLabels).map(([value, label]) => ({
            value,
            label,
          }))
        "
        filterable
        clearable
      /><n-select
        v-model:value="result"
        multiple
        max-tag-count="responsive"
        class="filter"
        placeholder="全部结果"
        aria-label="审计结果筛选"
        :options="
          Object.keys(resultLabels)
            .filter((value) => value !== 'skipped')
            .map((value) => ({
              value,
              label: resultLabels[value],
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
      :scroll-x="1160"
      ><template #empty
        ><n-empty
          description="暂无符合条件的审计记录"
          style="padding: 36px" /></template
    ></n-data-table>
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
  <n-drawer
    v-model:show="detailsOpen"
    :width="760"
    style="max-width: 100vw"
    placement="right"
  >
    <n-drawer-content title="审计详情" closable>
      <template v-if="selected">
        <div class="row spread wrap audit-detail-heading">
          <h2>{{ actionLabels[selected.action] || "其他操作" }}</h2>
          <StatusBadge
            :value="selected.display_result || selected.result"
            :label="
              resultLabels[selected.display_result || selected.result] ||
              '未知状态'
            "
          />
        </div>
        <dl class="audit-detail-fields">
          <dt>操作目标</dt>
          <dd>{{ selected.target_label || "未记录目标" }}</dd>
          <dt>操作时间</dt>
          <dd>
            {{
              new Date(selected.created_at).toLocaleString("zh-CN", {
                hour12: false,
              })
            }}
          </dd>
          <dt>操作人</dt>
          <dd>
            {{
              selected.actor === "anonymous"
                ? "未登录用户"
                : selected.actor || "未记录"
            }}
          </dd>
          <dt>来源 IP</dt>
          <dd class="mono">{{ selected.source_ip || "未记录" }}</dd>
          <dt>执行说明</dt>
          <dd>
            {{ selected.display_message || selected.message || "未记录说明" }}
          </dd>
        </dl>
        <n-alert
          v-if="selected.target_source !== 'snapshot'"
          type="info"
          class="audit-history-note"
          >历史记录未保存完整名称快照。已优先使用关联任务信息，缺失项使用当前查询结果，可能与操作时的名称不同。原始记录保持不变。</n-alert
        >
        <section v-if="detailTargets.length" class="audit-detail-section">
          <h3>
            目标明细<span class="muted"> · {{ detailTargets.length }} 项</span>
          </h3>
          <ul class="audit-target-list">
            <li v-for="(target, index) in detailTargets" :key="index">
              <div class="row spread wrap">
                <strong>{{ target.name }}</strong
                ><StatusBadge
                  v-if="target.result"
                  :value="target.result.status"
                  :label="resultLabels[target.result.status] || '未知状态'"
                />
              </div>
              <div class="audit-target-meta">
                <span>{{ targetKind(target) }}</span
                ><span v-if="target.account_name">{{
                  target.account_name
                }}</span
                ><span v-if="target.region">{{ target.region }}</span
                ><span v-if="target.instance_id" class="mono">{{
                  target.instance_id
                }}</span>
              </div>
              <p
                v-if="target.result?.error"
                :class="
                  target.result.status === 'failed' ? 'audit-failure' : 'muted'
                "
              >
                {{ target.result.error }}
              </p>
            </li>
          </ul>
        </section>
        <section
          v-if="selected.message || selected.task?.error"
          class="audit-detail-section"
        >
          <h3>详细说明</h3>
          <pre class="audit-raw">{{
            selected.task?.error || selected.message
          }}</pre>
        </section>
        <section class="audit-detail-section">
          <h3>排查标识</h3>
          <dl class="audit-detail-fields">
            <dt>审计编号</dt>
            <dd>#{{ selected.id }}</dd>
            <dt>操作代码</dt>
            <dd class="mono">{{ selected.action }}</dd>
            <dt>原始结果</dt>
            <dd class="mono">{{ selected.result }}</dd>
            <dt>原始目标</dt>
            <dd>
              <pre class="audit-raw">{{ selected.target || "未记录" }}</pre>
            </dd>
            <dt>记录请求 ID</dt>
            <dd class="mono">{{ selected.request_id || "未记录" }}</dd>
            <template v-if="selected.task"
              ><dt>任务编号</dt>
              <dd>#{{ selected.task.id }}</dd>
              <dt>云端请求 ID</dt>
              <dd class="mono">
                {{ selected.task.request_id || "未返回" }}
              </dd></template
            >
          </dl>
        </section>
      </template>
    </n-drawer-content>
  </n-drawer>
</template>
<style scoped>
.audit-target-name {
  font-weight: 500;
  overflow-wrap: anywhere;
}
.audit-detail-heading {
  margin-bottom: 20px;
}
.audit-detail-heading h2 {
  font-size: 18px;
  margin: 0;
}
.audit-detail-fields {
  display: grid;
  grid-template-columns: 100px minmax(0, 1fr);
  gap: 12px 16px;
  line-height: 1.7;
  margin: 0;
}
.audit-detail-fields dt {
  color: #667085;
}
.audit-detail-fields dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.audit-history-note {
  margin-top: 20px;
}
.audit-detail-section {
  margin-top: 28px;
}
.audit-detail-section h3 {
  margin: 0 0 12px;
  font-size: 14px;
}
.audit-target-list {
  padding: 0;
  margin: 0;
  list-style: none;
}
.audit-target-list li {
  padding: 14px 0;
  border-bottom: 1px solid #eef0f4;
  overflow-wrap: anywhere;
}
.audit-target-list strong {
  font-weight: 500;
}
.audit-target-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 12px;
  color: #667085;
  font-size: 12px;
  margin-top: 6px;
}
.audit-target-list p {
  white-space: pre-wrap;
  margin: 8px 0 0;
  line-height: 1.7;
}
.audit-failure {
  color: #b42318;
}
.audit-raw {
  font: inherit;
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  line-height: 1.7;
}
@media (max-width: 520px) {
  .audit-detail-fields {
    grid-template-columns: 1fr;
    gap: 4px;
  }
  .audit-detail-fields dd {
    margin-bottom: 10px;
  }
}
</style>
