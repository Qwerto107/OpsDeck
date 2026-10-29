<script setup lang="ts">
import { ref } from "vue";
import { NButton, NCard, NModal } from "naive-ui";
import { RefreshCw } from "lucide-vue-next";
import type { ResourceSyncState } from "../useResourceSync";

withDefaults(
  defineProps<{
    state: ResourceSyncState;
    mode?: "toolbar" | "notice";
    compact?: boolean;
    scopeLabel?: string;
  }>(),
  { mode: "toolbar", compact: false },
);
defineEmits<{ refresh: []; retry: [] }>();
const reasonsOpen = ref(false);
</script>

<template>
  <div
    v-if="mode === 'toolbar'"
    class="resource-sync"
    :aria-busy="state.loading"
    :title="scopeLabel"
  >
    <div class="resource-sync-info" role="status" aria-live="polite">
      <span v-if="state.loading" class="resource-sync-label"
        >已用 {{ state.elapsedSeconds }} 秒<span class="refresh-retained">
          · 刷新期间继续显示旧数据</span
        ></span
      >
      <template v-else-if="state.success">
        <span class="resource-sync-done">{{
          state.updated ? "已更新" : "已跳过"
        }}</span>
        <span class="resource-sync-label">{{ state.success }}</span>
      </template>
      <span
        v-else
        class="resource-sync-label"
        :class="{ 'is-expired': state.expired }"
      >
        <span v-if="state.expired" class="sample-dot" aria-hidden="true" />{{
          state.label === "尚无监控采样"
            ? ""
            : compact
              ? "采样时间"
              : "监控采样"
        }}
        {{ state.label }}
      </span>
    </div>
    <n-button
      class="resource-sync-button"
      :class="{ 'is-refreshing': state.loading }"
      :type="state.expired && !state.loading ? 'primary' : 'default'"
      :loading="state.loading"
      :disabled="state.loading || state.disabled"
      @click="$emit('refresh')"
    >
      <template #icon><RefreshCw :size="15" /></template
      >{{ state.loading ? "刷新中…" : compact ? "刷新" : "刷新监控" }}
    </n-button>
  </div>
  <div v-else class="resource-sync-notices">
    <div
      v-if="state.failures.length"
      class="resource-sync-banner is-failed"
      role="alert"
    >
      <span
        ><strong>{{ state.failures.length }} 台实例刷新失败</strong
        >，仍显示旧数据</span
      >
      <div class="resource-sync-actions">
        <n-button size="small" @click="reasonsOpen = true">查看原因</n-button>
        <n-button size="small" :disabled="state.loading" @click="$emit('retry')"
          >重试失败项</n-button
        >
      </div>
    </div>
    <div v-if="state.error" class="resource-sync-banner is-failed" role="alert">
      <span>{{ state.error }}；继续显示已有数据。</span>
      <n-button size="small" :disabled="state.loading" @click="$emit('refresh')"
        >重试刷新</n-button
      >
    </div>
    <div
      v-if="
        state.expired &&
        !state.loading &&
        !state.failures.length &&
        !state.error
      "
      class="resource-sync-banner is-stale"
      role="status"
    >
      <span
        ><strong>{{ state.staleText.split("，")[0] }}</strong
        >，下方为旧数据</span
      >
      <n-button
        size="small"
        :disabled="state.disabled"
        @click="$emit('refresh')"
        >立即刷新</n-button
      >
    </div>
    <n-modal v-model:show="reasonsOpen">
      <n-card
        title="监控刷新失败原因"
        class="refresh-reasons"
        :bordered="false"
        role="dialog"
        aria-modal="true"
        aria-label="监控刷新失败原因"
        closable
        @close="reasonsOpen = false"
      >
        <ul>
          <li v-for="failure in state.failures" :key="failure.resource_id">
            <strong>{{ failure.name || failure.instance_id }}</strong
            ><span class="muted">{{ failure.instance_id }}</span>
            <p>{{ failure.error || "未取得新的监控采样，请稍后重试。" }}</p>
          </li>
        </ul>
        <template #footer
          ><n-button
            :disabled="state.loading"
            type="primary"
            @click="
              reasonsOpen = false;
              $emit('retry');
            "
            >重试失败项</n-button
          ></template
        >
      </n-card>
    </n-modal>
  </div>
</template>

<style scoped>
.resource-sync,
.resource-sync-info,
.resource-sync-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  min-width: 0;
}
.resource-sync {
  justify-content: flex-end;
}
.resource-sync-info {
  gap: 8px;
}
.resource-sync-button {
  min-height: 34px;
  border-radius: 8px;
}
.resource-sync-label {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  color: #5b6678;
  font-size: 12px;
  line-height: 1.6;
}
.resource-sync-label.is-expired {
  color: #8a4b00;
}
.sample-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  flex-shrink: 0;
}
.resource-sync-done {
  color: #12703a;
  background: #e3f5ea;
  border-radius: 6px;
  padding: 2px 8px;
  font-size: 12px;
  font-weight: 500;
}
.resource-sync-button.is-refreshing {
  color: #5b6678;
  background: #f3f4f6;
}
.resource-sync-notices:empty {
  display: none;
}
.resource-sync-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  border-radius: 12px;
  padding: 12px 16px;
  margin: 0 0 16px;
  line-height: 1.6;
}
.resource-sync-banner.is-stale {
  background: #fff1d9;
  color: #8a4b00;
}
.resource-sync-banner.is-failed {
  background: #fde8e6;
  color: #8f1d14;
}
.refresh-reasons {
  width: min(560px, calc(100vw - 32px));
  max-height: 80vh;
  overflow-y: auto;
}
.refresh-reasons ul {
  padding: 0;
  margin: 0;
  list-style: none;
}
.refresh-reasons li {
  padding: 12px 0;
  border-bottom: 1px solid #eef0f4;
  overflow-wrap: anywhere;
}
.refresh-reasons li > span {
  display: block;
  font-size: 12px;
  margin-top: 3px;
}
.refresh-reasons p {
  margin: 8px 0 0;
  line-height: 1.6;
  color: #8f1d14;
}
@media (max-width: 640px) {
  .resource-sync {
    justify-content: flex-start;
    width: 100%;
  }
  .resource-sync-info {
    flex: 1;
  }
  .refresh-retained {
    display: none;
  }
  .resource-sync-actions {
    width: 100%;
  }
}
@media (prefers-reduced-motion: reduce) {
  .resource-sync :deep(.n-base-loading .n-base-loading__icon) {
    animation: none;
  }
}
</style>
