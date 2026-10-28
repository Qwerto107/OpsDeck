import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
  it("进入页面触发任务，合并重复点击，完成后读取快照", async () => {
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
    const sync = useResourceSync(load, () => ({ account_id: 2 }));
    await hooks.mounted[0]!();
    await vi.advanceTimersByTimeAsync(0);
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
  it("页面可见时按设置触发，隐藏或离开后停止触发", async () => {
    request.mockResolvedValue({ tasks: [] });
    useResourceSync(vi.fn().mockResolvedValue(undefined), () => ({}));
    await hooks.mounted[0]!();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(60000);
    expect(request).toHaveBeenCalledTimes(2);
    vi.stubGlobal("document", { hidden: true });
    await vi.advanceTimersByTimeAsync(60000);
    expect(request).toHaveBeenCalledTimes(2);
    hooks.unmounted.forEach((fn) => fn());
    vi.stubGlobal("document", { hidden: false });
    await vi.advanceTimersByTimeAsync(60000);
    expect(request).toHaveBeenCalledTimes(2);
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
    );
    await hooks.mounted[0]!();
    await vi.advanceTimersByTimeAsync(0);
    expect(sync.syncError.value).toBe("地域查询失败");
    expect(sync.syncStatus.value).toContain("失败");
  });
});
