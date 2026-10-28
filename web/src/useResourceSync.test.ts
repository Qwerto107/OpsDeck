import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ref } from "vue";
import type { ResourceCache } from "./types";
import { useResourceSync } from "./useResourceSync";

const hooks = vi.hoisted(() => ({
  mounted: [] as (() => unknown)[],
  unmounted: [] as (() => unknown)[],
}));
const request = vi.hoisted(() => vi.fn());
vi.mock("./api", () => ({ api: request }));
vi.mock("./store", () => ({
  useSession: () => ({ settings: { refresh_seconds: 60 } }),
}));
vi.mock("vue", async (original) => ({
  ...(await original<typeof import("vue")>()),
  onMounted: (fn: () => unknown) => hooks.mounted.push(fn),
  onUnmounted: (fn: () => unknown) => hooks.unmounted.push(fn),
}));

beforeEach(() => {
  vi.useFakeTimers();
  hooks.mounted = [];
  hooks.unmounted = [];
  request.mockReset();
  vi.stubGlobal("document", { hidden: false });
});
afterEach(() => {
  hooks.unmounted.forEach((fn) => fn());
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("页面触发云端同步", () => {
  it("手动触发任务，合并重复点击，完成后读取快照", async () => {
    const load = vi.fn().mockResolvedValue(undefined);
    request
      .mockResolvedValueOnce({
        tasks: [{ id: 7, status: "queued", error: "" }],
      })
      .mockResolvedValueOnce({
        tasks: [{ id: 7, status: "submitting", error: "" }],
      })
      .mockResolvedValueOnce({
        tasks: [{ id: 7, status: "succeeded", error: "" }],
      });
    const sync = useResourceSync(
      load,
      () => ({ account_id: 2 }),
      ref<ResourceCache>(),
    );
    await sync.refresh();
    expect(request).toHaveBeenCalledWith("/resources/sync", "POST", {
      account_id: 2,
    });
    await sync.refresh();
    expect(
      request.mock.calls.filter(([path]) => path === "/resources/sync"),
    ).toHaveLength(1);
    expect(load).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(2000);
    expect(load).toHaveBeenCalledTimes(1);
    expect(sync.syncing.value).toBe(false);
    expect(sync.syncError.value).toBe("");
  });
  it("进入页面和缓存过期不触发云请求，手动更新才回源", async () => {
    const cache = ref<ResourceCache>({
      updated_at: new Date().toISOString(),
      ttl_seconds: 180,
    });
    const sync = useResourceSync(vi.fn(), () => ({}), cache);
    hooks.mounted.forEach((fn) => fn());
    expect(sync.cacheLabel.value).toContain("缓存有效");
    await vi.advanceTimersByTimeAsync(180001);
    expect(sync.cacheLabel.value).toContain("缓存已过期");
    expect(request).not.toHaveBeenCalled();
    request.mockResolvedValue({ tasks: [] });
    await sync.refresh();
    expect(request).toHaveBeenCalledWith("/resources/sync", "POST", {});
  });
  it("同步失败显示原因，不冒充更新成功", async () => {
    request
      .mockResolvedValueOnce({ tasks: [{ id: 8, status: "queued" }] })
      .mockResolvedValueOnce({
        tasks: [{ id: 8, status: "failed", error: "地域查询失败" }],
      });
    const sync = useResourceSync(
      vi.fn().mockResolvedValue(undefined),
      () => ({}),
      ref<ResourceCache>(),
    );
    await sync.refresh();
    await vi.advanceTimersByTimeAsync(0);
    expect(sync.syncError.value).toBe("地域查询失败");
    expect(sync.syncStatus.value).toContain("失败");
  });
});
