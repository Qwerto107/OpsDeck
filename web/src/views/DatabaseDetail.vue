<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute, RouterLink } from "vue-router";
import { NAlert, NButton, NEmpty, NSkeleton } from "naive-ui";
import { api } from "../api";
import { useResourceSync } from "../useResourceSync";
import type { Resource, ResourceCache } from "../types";
import StatusBadge from "../components/StatusBadge.vue";
const route = useRoute(),
  resource = ref<Resource | null>(null),
  cache = ref<ResourceCache>(),
  error = ref(""),
  busy = ref(false);
async function load() {
  busy.value = true;
  error.value = "";
  try {
    const data = await api<{ resource: Resource; cache: ResourceCache }>(
      "/resources/" + route.params.id,
    );
    if (!data.resource.database) throw new Error("该资源不是数据库实例");
    resource.value = data.resource;
    cache.value = data.cache;
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
const networkLabels: Record<string, string> = {
  public: "公网",
  private: "内网",
  unknown: "未识别网络",
};
onMounted(load);
</script>
<template>
  <RouterLink to="/databases">返回数据库列表</RouterLink>
  <n-alert
    v-if="error"
    type="error"
    class="error-banner"
    style="margin-top: 16px"
    >{{ error }} <n-button text @click="load">重试</n-button></n-alert
  ><n-alert v-if="syncError" type="warning" class="error-banner">{{
    syncError
  }}</n-alert
  ><n-alert v-if="syncStatus" type="info" class="error-banner">{{
    syncStatus
  }}</n-alert
  ><n-skeleton
    v-if="busy && !resource"
    height="260px"
    style="margin-top: 16px"
  />
  <template v-if="resource"
    ><div class="panel" style="margin-top: 16px">
      <div class="row spread wrap">
        <div>
          <div class="row">
            <h1 class="page-title">{{ resource.name }}</h1>
            <StatusBadge :value="resource.state" />
          </div>
          <p class="muted mono">{{ resource.instance_id }}</p>
        </div>
        <div class="row wrap">
          <span class="muted small">{{ cacheLabel }}</span
          ><n-button :loading="syncing" @click="refresh">更新</n-button>
        </div>
      </div>
    </div>
    <div class="panel">
      <h2>基础信息</h2>
      <dl class="database-info">
        <dt>所属云账号</dt>
        <dd>{{ resource.account_name }}</dd>
        <dt>地域</dt>
        <dd>{{ resource.region }}</dd>
        <dt>引擎 / 版本</dt>
        <dd>
          {{ resource.database?.engine }}
          {{ resource.database?.version || "—" }}
        </dd>
        <dt>实例规格</dt>
        <dd>{{ resource.spec || "—" }}</dd>
        <dt>CPU / 内存</dt>
        <dd>
          {{ resource.cpu || "—" }} 核 /
          {{ resource.database?.memory_gb || "—" }} GB
        </dd>
        <dt>存储容量</dt>
        <dd>{{ resource.disk || "—" }} GB</dd>
        <dt>标签</dt>
        <dd>
          {{
            resource.tags_error ||
            Object.entries(resource.tags || {})
              .map(([key, value]) => `${key}: ${value}`)
              .join("；") ||
            "无标签"
          }}
        </dd>
        <dt>快照时间</dt>
        <dd>{{ new Date(resource.synced_at).toLocaleString() }}</dd>
      </dl>
    </div>
    <div class="panel">
      <h2>连接信息</h2>
      <div
        v-for="(endpoint, i) in resource.database?.endpoints"
        :key="i"
        class="database-endpoint"
      >
        <span class="muted">{{
          networkLabels[endpoint.network] || endpoint.network
        }}</span
        ><span class="mono"
          >{{ endpoint.address
          }}{{ endpoint.port ? ":" + endpoint.port : "" }}</span
        >
      </div>
      <n-empty
        v-if="!resource.database?.endpoints.length"
        description="云端未返回可用连接地址"
      /></div
  ></template>
</template>
<style scoped>
.database-info {
  display: grid;
  grid-template-columns: 110px minmax(0, 1fr);
  gap: 14px;
  margin-top: 20px;
}
.database-info dt {
  color: #667085;
}
.database-info dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.database-endpoint {
  display: flex;
  gap: 20px;
  flex-wrap: wrap;
  padding: 14px 0;
  overflow-wrap: anywhere;
}
.panel h2 {
  font-size: 14px;
}
</style>
