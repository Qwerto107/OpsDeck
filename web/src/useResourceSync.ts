import { onMounted, onUnmounted, ref, watch } from "vue";
import { api } from "./api";
import { useSession } from "./store";

interface SyncTask {
  id: number;
  status: string;
  error: string;
}
interface Progress {
  tasks: SyncTask[];
}

export function useResourceSync(
  load: () => Promise<void>,
  target: () => { account_id?: number | null; resource_id?: number },
) {
  const session = useSession();
  const syncing = ref(false),
    syncStatus = ref(""),
    syncError = ref("");
  let active = true;
  let interval: ReturnType<typeof setInterval> | undefined;
  let pollTimer: ReturnType<typeof setTimeout> | undefined;

  async function refresh() {
    if (!active || syncing.value || document.hidden) return;
    syncing.value = true;
    syncError.value = "";
    syncStatus.value = "正在请求更新…";
    try {
      const result = await api<Progress>("/resources/sync", "POST", target());
      if (!active) return;
      if (!result.tasks.length) {
        syncing.value = false;
        syncStatus.value = "没有已启用的授权云账号";
        await load();
        return;
      }
      await poll(result.tasks.map((task) => task.id));
    } catch (e) {
      if (!active) return;
      syncing.value = false;
      syncStatus.value = "更新失败";
      syncError.value = (e as Error).message;
    }
  }

  async function poll(ids: number[]) {
    if (!active) return;
    try {
      const { tasks } = await api<Progress>("/sync-tasks?ids=" + ids.join(","));
      if (!active) return;
      const pending = tasks.some(
        (task) => !["succeeded", "failed"].includes(task.status),
      );
      if (pending) {
        syncStatus.value = tasks.some((task) => task.status === "submitting")
          ? "正在更新云端数据…"
          : "同步排队中…";
        pollTimer = setTimeout(() => void poll(ids), 2000);
        return;
      }
      // 只有任务完成才读取新快照，避免把旧数据误报为已更新。
      await load();
      if (!active) return;
      syncError.value = [
        ...new Set(
          tasks
            .filter((task) => task.status === "failed")
            .map((task) => task.error || "云端同步失败"),
        ),
      ].join("；");
      syncStatus.value = syncError.value
        ? "部分或全部更新失败"
        : "已更新 " + new Date().toLocaleTimeString("zh-CN", { hour12: false });
      syncing.value = false;
    } catch (e) {
      if (!active) return;
      syncing.value = false;
      syncStatus.value = "无法确认更新结果";
      syncError.value = (e as Error).message;
    }
  }

  watch(
    () => session.settings.refresh_seconds,
    (seconds) => {
      clearInterval(interval);
      interval = setInterval(() => void refresh(), seconds * 1000);
    },
    { immediate: true },
  );
  onMounted(() => void refresh());
  onUnmounted(() => {
    active = false;
    clearInterval(interval);
    clearTimeout(pollTimer);
  });
  return { refresh, syncing, syncStatus, syncError };
}
