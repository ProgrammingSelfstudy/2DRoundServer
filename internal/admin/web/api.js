import { zhError } from "/lib.js";

// Calls the admin API. The token lives in sessionStorage (gone when the tab closes),
// is sent only as a bearer header, and is dropped on the first 401.

export class ApiError extends Error {
  constructor(status, message, retryAfter) {
    super(zhError(message));
    this.raw = message;
    this.status = status;
    this.retryAfter = retryAfter;
  }
}

const KEY = "admin.session";
let current = null;
try { current = JSON.parse(sessionStorage.getItem(KEY)); } catch { current = null; }

export const session = {
  get token() { return current && current.token; },
  get admin() { return current && current.admin; },
  set(token, admin) {
    current = { token, admin };
    try { sessionStorage.setItem(KEY, JSON.stringify(current)); } catch { /* private mode: keep it in memory */ }
  },
  update(admin) { if (current) this.set(current.token, admin); },
  clear() {
    current = null;
    try { sessionStorage.removeItem(KEY); } catch { /* ignore */ }
  },
};

export async function api(method, path, body) {
  const headers = { Accept: "application/json" };
  if (session.token) headers.Authorization = "Bearer " + session.token;
  let payload;
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch(path, { method, headers, body: payload, cache: "no-store", credentials: "omit" });
  } catch {
    throw new ApiError(0, "无法连接到服务器");
  }
  let data = null;
  try { data = await res.json(); } catch { /* an empty or non-JSON body */ }
  if (!res.ok) {
    if (res.status === 401 && session.token) {
      session.clear();
      window.dispatchEvent(new Event("session-expired"));
    }
    throw new ApiError(res.status, (data && data.error) || `请求失败（${res.status}）`, Number(res.headers.get("Retry-After")) || 0);
  }
  return data;
}

export const get = (p) => api("GET", p);
export const post = (p, b) => api("POST", p, b ?? {});
export const put = (p, b) => api("PUT", p, b);
export const patch = (p, b) => api("PATCH", p, b);
export const del = (p) => api("DELETE", p);
