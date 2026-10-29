import { afterEach, describe, expect, it, vi } from "vitest";
import { api, query, setCSRF } from "./api";

afterEach(() => {
  vi.unstubAllGlobals();
  setCSRF("");
});

describe("API 请求边界", () => {
  it("向写入请求附加 CSRF 和同源会话，不把凭证放入 URL", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetch);
    setCSRF("test-csrf");
    await api("/preferences", "PUT", { columns: ["state"] });
    expect(fetch).toHaveBeenCalledWith(
      "/api/preferences",
      expect.objectContaining({
        credentials: "same-origin",
        method: "PUT",
        headers: expect.objectContaining({ "X-CSRF-Token": "test-csrf" }),
        body: '{"columns":["state"]}',
      }),
    );
  });
  it("首次检查登录状态时不把未登录误报为会话过期", async () => {
    const dispatchEvent = vi.fn();
    vi.stubGlobal("window", { dispatchEvent });
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response('{"error":"请先登录"}', { status: 401 }),
        ),
    );
    await expect(api("/me")).rejects.toThrow("请先登录");
    expect(dispatchEvent).not.toHaveBeenCalled();
  });
  it("保护接口返回 401 时通知界面退出登录", async () => {
    const dispatchEvent = vi.fn();
    vi.stubGlobal("window", { dispatchEvent });
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response('{"error":"会话失效"}', { status: 401 }),
        ),
    );
    await expect(api("/resources")).rejects.toThrow("会话失效");
    expect(dispatchEvent).toHaveBeenCalledTimes(1);
  });
  it("首次身份绑定的密码错误保留在向导中", async () => {
    const dispatchEvent = vi.fn();
    vi.stubGlobal("window", { dispatchEvent });
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response('{"error":"账号或密码错误"}', { status: 401 }),
        ),
    );
    await expect(
      api("/oauth/bind", "POST", {
        username: "existing",
        password: "wrong",
        confirm: true,
      }),
    ).rejects.toThrow("账号或密码错误");
    expect(dispatchEvent).not.toHaveBeenCalled();
  });
  it("正确编码标签排序条件和特殊搜索字符，并保留数字零", () => {
    const params = new URLSearchParams(
      query({
        q: "a&b=测试",
        sort: '[{"field":"tag","key":"team/name"}]',
        page: 0,
        region: null,
        empty: "",
      }),
    );
    expect(params.get("q")).toBe("a&b=测试");
    expect(params.get("sort")).toBe('[{"field":"tag","key":"team/name"}]');
    expect(params.get("page")).toBe("0");
    expect(params.has("region")).toBe(false);
  });
});
