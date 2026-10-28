<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { NButton, NAlert, NSkeleton, NEmpty, NTag } from "naive-ui";

import type { Resource, Task, ResourceCache } from "../types";
import { api } from "../api";
import { useResourceSync } from "../useResourceSync";
import StatusBadge from "../components/StatusBadge.vue";
import MetricValue from "../components/MetricValue.vue";
import ActionModal from "../components/ActionModal.vue";
const route = useRoute(),
  resource = ref<Resource | null>(null),
  cache = ref<ResourceCache>(),
  tasks = ref<Task[]>([]),
  error = ref(""),
  busy = ref(false),
  modal = ref(false),
  selected = ref("");
const actions = [
  ["start", "开机"],
  ["stop", "关机"],
  ["reboot", "重启"],
];
const actionLabels: Record<string, string> = {
  start: "开机",
  stop: "关机",
  reboot: "重启",
  force_stop: "强制关机",
  force_reboot: "强制重启",
};
const metricOrder = [
  "cpu",
  "memory",
  "network_in",
  "network_out",
  "disk",
  "disk:mounts",
];
const metrics = computed(() =>
    Object.entries(resource.value?.metrics || {}).sort(
      ([a], [b]) => metricOrder.indexOf(a) - metricOrder.indexOf(b),
    ),
  ),
  labels: Record<string, string> = {
    cpu: "CPU 使用率",
    memory: "内存使用率",
    network_in: "网络入站（公网）",
    network_out: "网络出站（公网）",
    disk: "磁盘空间（实例级）",
    "disk:mounts": "磁盘挂载点明细",
  };
async function load() {
  busy.value = true;
  error.value = "";
  try {
    const data = await api<{
      resource: Resource;
      tasks: Task[];
      cache: ResourceCache;
    }>("/resources/" + route.params.id);
    resource.value = data.resource;
    cache.value = data.cache;
    tasks.value = data.tasks || [];
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
const { refresh, syncing, syncStatus, syncError, cacheLabel } = useResourceSync(
  load,
  () => ({ resource_id: Number(route.params.id) }),
  cache,
);
onMounted(() => void load());
</script>
<template>
  <div class="instance-detail">
    <n-alert v-if="syncError" type="warning"
      >{{ syncError }}
      <n-button text @click="refresh">重试更新</n-button></n-alert
    >
    <n-alert v-if="error" type="error"
      >{{ error }} <n-button text @click="load">重试</n-button></n-alert
    >
    <n-skeleton v-if="!resource && busy" height="260px" />
    <template v-if="resource">
      <section class="panel instance-header">
        <div class="instance-identity">
          <div class="row wrap">
            <h1>{{ resource.name }}</h1>
            <status-badge
              :value="resource.operation_status || resource.state"
            /><n-tag size="small" :bordered="false">{{
              resource.kind === "cvm" ? "CVM" : "Lighthouse"
            }}</n-tag>
          </div>
          <div class="muted instance-meta">
            {{ resource.instance_id }} · {{ resource.account_name }} ·
            {{ resource.region }}
          </div>
        </div>
        <div class="row wrap instance-actions">
          <template v-for="[action, label] in actions" :key="action">
            <n-button
              v-if="
                resource.allowed_actions?.includes(action) ||
                resource.allowed_actions?.includes(
                  action === 'stop'
                    ? 'force_stop'
                    : action === 'reboot'
                      ? 'force_reboot'
                      : '',
                )
              "
              @click="
                selected = action;
                modal = true;
              "
              >{{ label }}</n-button
            ></template
          >
        </div>
      </section>
      <div class="instance-panels">
        <section class="panel">
          <h2>基础信息</h2>
          <dl class="instance-info">
            <dt>规格</dt>
            <dd>{{ resource.spec || "—" }}</dd>
            <dt>配置</dt>
            <dd>
              {{ resource.cpu }} 核 · {{ resource.memory }} GB ·
              {{ resource.disk }} GB 云硬盘
            </dd>
            <dt>公网 IP</dt>
            <dd>{{ resource.public_ip || "无公网 IP" }}</dd>
            <dt>私网 IP</dt>
            <dd>{{ resource.private_ip || "无私网 IP" }}</dd>
            <dt>标签</dt>
            <dd class="tag-list">
              <span
                v-for="(v, k) in resource.tags"
                :key="k"
                class="resource-tag"
                >{{ k }}: {{ v }}</span
              ><span
                v-if="!Object.keys(resource.tags || {}).length"
                class="muted"
                >{{ resource.tags_error ? "标签获取失败" : "无标签" }}</span
              >
            </dd>
          </dl>
        </section>
        <section class="panel monitor-panel">
          <div class="row spread wrap">
            <h2>当前监控</h2>
            <div class="row wrap">
              <span class="muted monitor-time">{{ cacheLabel }}</span>
              <span
                v-if="syncStatus"
                class="muted monitor-time"
                role="status"
                >{{ syncStatus }}</span
              ><n-button size="small" :loading="syncing" @click="refresh"
                >更新</n-button
              >
            </div>
          </div>
          <table v-if="metrics.length" class="monitor-table">
            <thead>
              <tr>
                <th scope="col">指标</th>
                <th scope="col">当前值</th>
                <th scope="col">使用情况</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="[key, m] in metrics" :key="key">
                <td>
                  {{ labels[key] || key }}
                </td>
                <td><metric-value :metric="m" /></td>
                <td>
                  <div
                    v-if="m.unit === '%' && m.value != null"
                    class="meter"
                    :aria-label="labels[key] + ' ' + m.value + '%'"
                  >
                    <i
                      :class="{ high: m.value >= 80 }"
                      :style="{
                        width: Math.max(0, Math.min(100, m.value)) + '%',
                      }"
                    />
                  </div>
                  <span v-else class="muted">{{
                    m.value == null ? m.reason || "云监控未返回该指标" : "—"
                  }}</span>
                </td>
              </tr>
            </tbody>
          </table>
          <n-empty v-else description="尚无可展示的监控数据" />
        </section>
      </div>
      <section class="panel operation-records">
        <h2>操作记录</h2>
        <div v-for="task in tasks" :key="task.id" class="operation-record">
          <div>
            <span>{{ actionLabels[task.action] || task.action }}</span>
            <div v-if="task.error" class="hint">{{ task.error }}</div>
            <div v-if="task.request_id" class="hint request-id">
              {{ task.request_id }}
            </div>
          </div>
          <div class="row wrap">
            <time class="muted">{{
              new Date(task.created_at).toLocaleString("zh-CN", {
                hour12: false,
              })
            }}</time
            ><status-badge :value="task.status" />
          </div>
        </div>
        <n-empty v-if="!tasks.length" description="暂无操作记录" />
      </section>
      <action-modal
        v-model:show="modal"
        :resource="resource"
        :action="selected"
        @done="load"
      />
    </template>
  </div>
</template>
<style scoped>
.instance-detail {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.instance-detail .panel {
  padding: 16px;
  margin: 0;
  min-width: 0;
}
.instance-detail h2 {
  font-size: 13px;
  margin: 0;
}
.instance-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
}
.instance-header h1 {
  font-size: 18px;
  margin: 0;
  font-weight: 700;
}
.instance-meta {
  margin-top: 4px;
  overflow-wrap: anywhere;
}
.instance-actions {
  gap: 8px;
}

.instance-panels {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 16px;
}
.instance-info {
  display: grid;
  grid-template-columns: 90px minmax(0, 1fr);
  gap: 10px 8px;
  margin: 12px 0 0;
}
.instance-info dt {
  color: #5b6678;
}
.instance-info dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.monitor-time {
  font-size: 12px;
}
.monitor-table {
  border-collapse: collapse;
  width: 100%;
  margin-top: 12px;
  table-layout: fixed;
}
.monitor-table th {
  background: #f9fafb;
  color: #5b6678;
  font-weight: 500;
  padding: 8px 6px;
  text-align: left;
}
.monitor-table td {
  padding: 10px 6px;
  overflow-wrap: anywhere;
}
.monitor-table th,
.monitor-table td {
  border-bottom: 1px solid #f0f2f6;
}
.monitor-table th:last-child {
  width: 40%;
}
.monitor-table .meter {
  margin: 0;
  height: 6px;
  width: 100%;
}
.operation-records h2 {
  margin-bottom: 8px;
}
.operation-record {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  padding: 8px 0;
  border-bottom: 1px solid #f0f2f6;
}
.operation-record:last-child {
  border: 0;
}
.request-id {
  overflow-wrap: anywhere;
}
@media (max-width: 1000px) {
  .instance-panels {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 720px) {
  .operation-record {
    flex-wrap: wrap;
  }
}
</style>
