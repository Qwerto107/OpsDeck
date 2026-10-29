let csrfToken = "";
export function setCSRF(value: string) {
  csrfToken = value;
}
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
  headers: Record<string, string> = {},
): Promise<T> {
  const response = await fetch("/api" + path, {
    method,
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      "X-CSRF-Token": csrfToken,
      ...headers,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) {
    const data = await response
      .json()
      .catch(() => ({ error: "网络请求未完成" }));
    if (
      response.status === 401 &&
      path !== "/login" &&
      path !== "/me" &&
      path !== "/oauth/bind"
    )
      window.dispatchEvent(new Event("session-expired"));
    throw new Error(data.error || "请求失败");
  }
  if (response.status === 204 || response.headers.get("content-length") === "0")
    return undefined as T;
  const text = await response.text();
  return (text ? JSON.parse(text) : undefined) as T;
}
export function query(params: Record<string, unknown>) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params))
    if (v !== "" && v != null) q.set(k, String(v));
  return "?" + q.toString();
}
