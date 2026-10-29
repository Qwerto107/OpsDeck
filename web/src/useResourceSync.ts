import { computed, onUnmounted, ref, watch, type Ref } from "vue";
import { api } from "./api";
import type { RefreshResult, ResourceCache } from "./types";

interface SyncTask {
  id: number;
  status: string;
  error?: string;
  results?: RefreshResult[] | null;
}
interface Progress {
  tasks: SyncTask[];
}
export interface ResourceSyncState {
  loading: boolean;
  expired: boolean;
  label: string;
  staleText: string;
  elapsedSeconds: number;
  success: string;
  updated: boolean;
  failures: RefreshResult[];
  error: string;
  disabled: boolean;
}

export function useResourceSync(
  load: () => Promise<void>,
  target: () => {
    account_id?: number | null;
    resource_id?: number;
    resource_ids?: number[];
  },
  cache: Ref<ResourceCache | undefined>,
) {
  const syncing = ref(false),
    error = ref(""),
    results = ref<RefreshResult[]>([]);
  const now = ref(Date.now()),
    startedAt = ref(0),
    completedAt = ref(0);
  let active = true,
    generation = 0,
    nextAutoAttempt = 0;
  let pollTimer: ReturnType<typeof setTimeout> | undefined;
  const clock = setInterval(() => {
    now.value = Date.now();
  }, 1000);
  watch(cache, () => {
    now.value = Date.now();
  });

  function reset() {
    generation++;
    clearTimeout(pollTimer);
    syncing.value = false;
    error.value = "";
    results.value = [];
    completedAt.value = 0;
    nextAutoAttempt = 0;
  }
  function postponeAutoRefresh() {
    nextAutoAttempt = Date.now() + (cache.value?.ttl_seconds || 600) * 1000;
  }
  // 翻页或切换筛选范围后，不把上一批任务的结果显示成当前页的结果。
  watch(() => {
    const scope = target();
    return JSON.stringify({
      ...scope,
      resource_ids: scope.resource_ids?.slice().sort((a, b) => a - b),
    });
  }, reset);

  async function refresh(onlyFailed = false) {
    if (!active || syncing.value) return;
    const selection = { ...target() };
    if (onlyFailed)
      selection.resource_ids = results.value
        .filter((r) => r.status === "failed")
        .map((r) => r.resource_id);
    if (selection.resource_ids?.length === 0) return;
    if (onlyFailed) delete selection.resource_id;
    const run = ++generation;
    syncing.value = true;
    error.value = "";
    results.value = [];
    completedAt.value = 0;
    startedAt.value = now.value = Date.now();
    try {
      const response = await api<Progress>(
        "/resources/sync",
        "POST",
        selection,
      );
      if (!active || run !== generation) return;
      if (!response.tasks.length) throw new Error("没有可刷新的实例");
      await poll(
        response.tasks.map((task) => task.id),
        run,
      );
    } catch (e) {
      if (!active || run !== generation) return;
      postponeAutoRefresh();
      syncing.value = false;
      error.value = (e as Error).message;
    }
  }

  async function poll(ids: number[], run: number) {
    try {
      const { tasks } = await api<Progress>("/sync-tasks?ids=" + ids.join(","));
      if (!active || run !== generation) return;
      if (tasks.length !== ids.length)
        throw new Error("刷新任务不存在或已过期");
      if (
        tasks.some((task) => !["succeeded", "failed"].includes(task.status))
      ) {
        pollTimer = setTimeout(() => void poll(ids, run), 2000);
        return;
      }
      // 完成前一直展示原快照；结束后读取成功项，失败项由后端保留旧数据。
      await load();
      if (!active || run !== generation) return;
      results.value = tasks.flatMap((task) => task.results || []);
      error.value = [
        ...new Set(
          tasks
            .filter(
              (task) =>
                task.status === "failed" &&
                !task.results?.some((result) => result.status === "failed"),
            )
            .map((task) => task.error || "刷新失败，无法确认结果"),
        ),
      ].join("；");
      completedAt.value = now.value = Date.now();
      postponeAutoRefresh();
      syncing.value = false;
    } catch (e) {
      if (!active || run !== generation) return;
      postponeAutoRefresh();
      syncing.value = false;
      error.value = "无法确认刷新结果：" + (e as Error).message;
    }
  }

  const sampledAt = computed(() =>
    cache.value?.sampled_at ? Date.parse(cache.value.sampled_at) : 0,
  );
  const age = computed(() => Math.max(0, now.value - sampledAt.value));
  const relativeTime = computed(() => {
    const minutes = Math.floor(age.value / 60000);
    if (minutes < 1) return "刚刚";
    if (minutes < 60) return `${minutes} 分钟前`;
    return `${Math.floor(minutes / 60)} 小时${minutes % 60 ? ` ${minutes % 60} 分钟` : ""}前`;
  });
  const expired = computed(
    () =>
      sampledAt.value > 0 &&
      age.value >= (cache.value?.stale_after_seconds || 600) * 1000,
  );
  const formatTime = (stamp: number) =>
    new Date(stamp).toLocaleTimeString("zh-CN", {
      hour12: false,
      hour: "2-digit",
      minute: "2-digit",
    });
  const syncState = computed<ResourceSyncState>(() => {
    const succeeded = results.value.filter(
      (r) => r.status === "succeeded",
    ).length;
    const skipped = results.value.filter((r) => r.status === "skipped").length;
    const failed = results.value.filter((r) => r.status === "failed");
    return {
      loading: syncing.value,
      expired: expired.value,
      label: sampledAt.value
        ? `${formatTime(sampledAt.value)} · ${relativeTime.value}`
        : "尚无监控采样",
      staleText: `监控数据已 ${Math.floor(age.value / 60000)} 分钟未刷新，下方为旧数据`,
      elapsedSeconds: Math.max(
        0,
        Math.floor((now.value - startedAt.value) / 1000),
      ),
      success:
        completedAt.value && !error.value && !expired.value
          ? succeeded > 0
            ? `${formatTime(completedAt.value)} · ${succeeded} 台实例刷新成功${skipped ? ` · ${skipped} 台已跳过` : ""}`
            : skipped > 0
              ? `${skipped} 台实例未运行或未启用监控，已跳过`
              : ""
          : "",
      updated: succeeded > 0,
      failures: failed,
      error: error.value,
      disabled: target().resource_ids?.length === 0,
    };
  });
  watch(
    [cache, now],
    () => {
      if (
        !active ||
        !cache.value?.auto_refresh ||
        syncing.value ||
        !target().resource_ids?.length ||
        now.value < nextAutoAttempt
      )
        return;
      const stamp = cache.value.monitor_updated_at
        ? Date.parse(cache.value.monitor_updated_at)
        : 0;
      const ttl = (cache.value.ttl_seconds || 600) * 1000;
      if (stamp > 0 && now.value < stamp + ttl) return;
      // 使用成功获取时间判断缓存；云端采样延迟或失败不会造成连续回源。
      postponeAutoRefresh();
      void refresh();
    },
    { immediate: true, flush: "post" },
  );
  onUnmounted(() => {
    active = false;
    reset();
    clearInterval(clock);
  });
  return {
    refresh: () => refresh(),
    retryFailed: () => refresh(true),
    syncState,
  };
}
