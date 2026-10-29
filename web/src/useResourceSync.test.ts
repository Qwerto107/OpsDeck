import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick, ref } from "vue";
import type { RefreshResult, ResourceCache } from "./types";
import { useResourceSync } from "./useResourceSync";

const hooks = vi.hoisted(() => ({ unmounted: [] as (() => unknown)[] }));
const request = vi.hoisted(() => vi.fn());
vi.mock("./api", () => ({ api: request }));
vi.mock("vue", async (original) => ({
  ...(await original<typeof import("vue")>()),
  onUnmounted: (fn: () => unknown) => hooks.unmounted.push(fn),
}));
const cacheAt = (minutes = 0): ResourceCache => ({
  updated_at: new Date().toISOString(),
  ttl_seconds: 600,
  sampled_at: new Date(Date.now() - minutes * 60000).toISOString(),
  stale_after_seconds: 600,
});
const result = (
  id: number,
  status: RefreshResult["status"],
): RefreshResult => ({
  resource_id: id,
  name: `实例 ${id}`,
  instance_id: `ins-${id}`,
  status,
  error: status === "failed" ? "云监控请求失败：UnauthorizedOperation" : "",
});

beforeEach(() => {
  vi.useFakeTimers();
  hooks.unmounted = [];
  request.mockReset();
});
afterEach(() => {
  hooks.unmounted.forEach((fn) => fn());
  vi.useRealTimers();
});

describe("手动监控刷新", () => {
  it("按实际采样时间提示过期，页面进入及过期不触发云请求", async () => {
    const cache = ref(cacheAt());
    const sync = useResourceSync(vi.fn(), () => ({ resource_ids: [1] }), cache);
    expect(sync.syncState.value.label).toContain("刚刚");
    await vi.advanceTimersByTimeAsync(600001);
    expect(sync.syncState.value.expired).toBe(true);
    expect(sync.syncState.value.label).toContain("10 分钟前");
    expect(request).not.toHaveBeenCalled();
    cache.value = { ...cacheAt(12), updated_at: new Date().toISOString() };
    await nextTick();
    expect(sync.syncState.value.expired).toBe(true);
    expect(sync.syncState.value.staleText).toContain("12 分钟未刷新");
  });
  it("刷新中显示经过秒数，合并重复点击并保留原快照", async () => {
    const load = vi.fn().mockResolvedValue(undefined);
    request.mockResolvedValue({ tasks: [{ id: 7, status: "submitting" }] });
    const sync = useResourceSync(
      load,
      () => ({ resource_ids: [1] }),
      ref(cacheAt(12)),
    );
    await sync.refresh();
    await sync.refresh();
    await vi.advanceTimersByTimeAsync(8000);
    expect(sync.syncState.value.loading).toBe(true);
    expect(sync.syncState.value.elapsedSeconds).toBe(8);
    expect(load).not.toHaveBeenCalled();
    expect(
      request.mock.calls.filter(([path]) => path === "/resources/sync"),
    ).toHaveLength(1);
    request.mockResolvedValue({
      tasks: [
        { id: 7, status: "succeeded", results: [result(1, "succeeded")] },
      ],
    });
    await vi.advanceTimersByTimeAsync(2000);
    expect(load).toHaveBeenCalledTimes(1);
    expect(sync.syncState.value.loading).toBe(false);
    expect(sync.syncState.value.error).toBe("");
  });
  it("根据实例结果统计成功数量，仅重试失败项", async () => {
    request
      .mockResolvedValueOnce({ tasks: [{ id: 8, status: "queued" }] })
      .mockResolvedValueOnce({
        tasks: [
          {
            id: 8,
            status: "failed",
            results: [
              result(1, "succeeded"),
              result(2, "failed"),
              result(3, "failed"),
            ],
          },
        ],
      });
    const sync = useResourceSync(
      vi.fn().mockResolvedValue(undefined),
      () => ({ resource_ids: [1, 2, 3] }),
      ref(cacheAt()),
    );
    await sync.refresh();
    expect(sync.syncState.value.failures.map((r) => r.resource_id)).toEqual([
      2, 3,
    ]);
    expect(sync.syncState.value.success).toContain("1 台实例刷新成功");
    request
      .mockResolvedValueOnce({ tasks: [{ id: 9, status: "queued" }] })
      .mockResolvedValueOnce({
        tasks: [
          {
            id: 9,
            status: "succeeded",
            results: [result(2, "succeeded"), result(3, "succeeded")],
          },
        ],
      });
    await sync.retryFailed();
    expect(request).toHaveBeenCalledWith("/resources/sync", "POST", {
      resource_ids: [2, 3],
    });
    expect(sync.syncState.value.failures).toHaveLength(0);
    expect(sync.syncState.value.success).toContain("2 台实例刷新成功");
  });
  it("翻页和离开页面后忽略上一批结果，空页面不创建任务", async () => {
    const ids = ref<number[]>([]),
      load = vi.fn();
    const sync = useResourceSync(
      load,
      () => ({ resource_ids: ids.value }),
      ref(cacheAt()),
    );
    expect(sync.syncState.value.disabled).toBe(true);
    await sync.refresh();
    expect(request).not.toHaveBeenCalled();
    ids.value = [1];
    await nextTick();
    request.mockResolvedValue({ tasks: [{ id: 7, status: "submitting" }] });
    await sync.refresh();
    ids.value = [2];
    await nextTick();
    await vi.advanceTimersByTimeAsync(3000);
    expect(sync.syncState.value.loading).toBe(false);
    expect(load).not.toHaveBeenCalled();
    hooks.unmounted.forEach((fn) => fn());
    expect(vi.getTimerCount()).toBe(0);
  });
  it("轮询中断时不冒充成功，不清空已有数据", async () => {
    const load = vi.fn();
    request
      .mockResolvedValueOnce({ tasks: [{ id: 8, status: "queued" }] })
      .mockRejectedValueOnce(new Error("连接中断"));
    const sync = useResourceSync(
      load,
      () => ({ resource_ids: [1] }),
      ref(cacheAt()),
    );
    await sync.refresh();
    expect(sync.syncState.value.error).toContain("无法确认刷新结果");
    expect(sync.syncState.value.success).toBe("");
    expect(load).not.toHaveBeenCalled();
  });
});

describe("自动监控刷新", () => {
  it("进入过期页面自动刷新，冷却期内不因旧采样重复回源", async () => {
    const cache = ref<ResourceCache | undefined>();
    request.mockResolvedValue({
      tasks: [
        { id: 8, status: "succeeded", results: [result(1, "succeeded")] },
      ],
    });
    const load = vi.fn(async () => {
      cache.value = {
        ...cacheAt(12),
        auto_refresh: true,
        monitor_updated_at: new Date().toISOString(),
      };
    });
    useResourceSync(load, () => ({ resource_ids: [1] }), cache);
    expect(request).not.toHaveBeenCalled();
    cache.value = {
      ...cacheAt(12),
      auto_refresh: true,
      monitor_updated_at: null,
    };
    await vi.advanceTimersByTimeAsync(1000);
    expect(load).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(598000);
    expect(
      request.mock.calls.filter(([path]) => path === "/resources/sync"),
    ).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(2000);
    expect(
      request.mock.calls.filter(([path]) => path === "/resources/sync"),
    ).toHaveLength(2);
  });

  it("有效缓存不因进入页面刷新，到期才刷新；关闭开关后停止", async () => {
    const cache = ref({
      ...cacheAt(),
      auto_refresh: true,
      monitor_updated_at: new Date().toISOString(),
    });
    request.mockResolvedValue({ tasks: [{ id: 7, status: "submitting" }] });
    useResourceSync(vi.fn(), () => ({ resource_ids: [1] }), cache);
    await vi.advanceTimersByTimeAsync(599000);
    expect(request).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1000);
    expect(request).toHaveBeenCalledWith("/resources/sync", "POST", {
      resource_ids: [1],
    });
    cache.value = { ...cache.value, auto_refresh: false };
    request.mockResolvedValue({
      tasks: [{ id: 7, status: "failed", error: "权限不足" }],
    });
    await vi.advanceTimersByTimeAsync(700000);
    expect(
      request.mock.calls.filter(([path]) => path === "/resources/sync"),
    ).toHaveLength(1);
  });

  it("失败后间隔缓存时长再尝试，离开页面后停止", async () => {
    request.mockRejectedValue(new Error("云接口不可用"));
    const sync = useResourceSync(
      vi.fn(),
      () => ({ resource_ids: [1] }),
      ref({ ...cacheAt(12), auto_refresh: true }),
    );
    await vi.advanceTimersByTimeAsync(1000);
    expect(sync.syncState.value.error).toContain("云接口不可用");
    await vi.advanceTimersByTimeAsync(598000);
    expect(request).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(2000);
    expect(request).toHaveBeenCalledTimes(2);
    hooks.unmounted.forEach((fn) => fn());
    await vi.advanceTimersByTimeAsync(700000);
    expect(request).toHaveBeenCalledTimes(2);
  });

  it("空列表及无监控权限时不自动创建任务", async () => {
    useResourceSync(
      vi.fn(),
      () => ({ resource_ids: [] }),
      ref({ ...cacheAt(12), auto_refresh: true }),
    );
    await vi.advanceTimersByTimeAsync(200000);
    expect(request).not.toHaveBeenCalled();
  });
});
