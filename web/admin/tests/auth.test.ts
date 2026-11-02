import test from "node:test";
import assert from "node:assert/strict";
import { AdminSession, SESSION_KEY } from "../src/auth/session.js";
import { can } from "../src/auth/permissions.js";
import { ApiError, createApi, StaleSessionError } from "../src/api/client.js";
const future = () => Math.floor(Date.now() / 1000) + 3600;
const identity = { id: 1, username: "tester", role: "viewer" as const, status: 1 };
function storage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) || null,
    setItem: (key: string, val: string) => {
      values.set(key, val);
    },
    removeItem: (key: string) => {
      values.delete(key);
    }
  };
}
function active() {
  const session = new AdminSession();
  session.start("test-token", future());
  session.confirm(identity, session.state.revision);
  return session;
}
test("three role levels; unknown roles/capabilities and prototype keys are denied", () => {
  assert.deepEqual(
    ["read", "write", "manage"].map(p => can("viewer", p)),
    [true, false, false]
  );
  assert.deepEqual(
    ["read", "write", "manage"].map(p => can("operator", p)),
    [true, true, false]
  );
  assert.deepEqual(
    ["read", "write", "manage"].map(p => can("superadmin", p)),
    [true, true, true]
  );
  for (const value of ["admin", "__proto__", "constructor", undefined, null]) {
    assert.equal(can(value, "read"), false);
    assert.equal(can("superadmin", value), false);
  }
});
test("sessionStorage restores token only; forged cached admin never grants permissions", () => {
  const disk = storage();
  const session = new AdminSession(disk);
  session.start("test-token", future());
  session.confirm(identity, session.state.revision);
  assert.equal(JSON.parse(disk.getItem(SESSION_KEY)!).admin, undefined);
  const cached = JSON.parse(disk.getItem(SESSION_KEY)!);
  cached.admin = { ...identity, role: "superadmin" };
  disk.setItem(SESSION_KEY, JSON.stringify(cached));
  const restored = new AdminSession(disk);
  assert.equal(restored.state.token, "test-token");
  assert.equal(restored.state.admin, null);
  restored.confirm(identity, restored.state.revision);
  assert.deepEqual(restored.state.admin, identity);
  restored.clear();
  assert.equal(disk.getItem(SESSION_KEY), null);
  assert.equal(restored.state.admin, null);
});
test("expired/corrupt storage and disabled/unknown administrator fail closed", () => {
  const disk = storage();
  disk.setItem(SESSION_KEY, "malformed");
  assert.equal(new AdminSession(disk).state.token, "");
  disk.setItem(SESSION_KEY, JSON.stringify({ token: "expired", expiresAt: 1 }));
  assert.equal(new AdminSession(disk).state.token, "");
  for (const admin of [
    { ...identity, status: 0 },
    { ...identity, role: "owner" }
  ]) {
    const session = active();
    assert.throws(() => session.confirm(admin, session.state.revision));
    assert.equal(session.state.token, "");
  }
  let now = 0;
  const timed = new AdminSession(undefined, () => now);
  timed.start("test-token", 2);
  now = 2000;
  assert.equal(timed.expire(), true);
  assert.equal(timed.state.token, "");
});
test("API uses local /api, bearer, raw Go response, no cookies or login bearer", async () => {
  const session = active();
  const seen: [string, RequestInit][] = [];
  const fetcher = (async (url: string, opts: RequestInit) => {
    seen.push([url, opts]);
    return Response.json({ accounts: 3 });
  }) as typeof fetch;
  const api = createApi(session, fetcher);
  assert.deepEqual(await api("/stats"), { accounts: 3 });
  assert.equal(seen[0][0], "/api/stats");
  assert.equal((seen[0][1].headers as Record<string, string>).Authorization, "Bearer test-token");
  assert.equal(seen[0][1].credentials, "omit");
  assert.equal(seen[0][1].redirect, "error");
  await api("/login", { method: "POST", public: true, body: { username: "x", password: "test-only" } });
  assert.equal((seen[1][1].headers as Record<string, string>).Authorization, undefined);
  await assert.rejects(api("https://example.com"));
  await assert.rejects(api("//example.com"));
});
test("502 keeps the exact gateway message of the module endpoints and words a failed push generically", async () => {
  const session = active();
  const gateway = (error: string) =>
    createApi(session, (async () => Response.json({ error }, { status: 502 })) as typeof fetch);
  // a reset states that nothing was written; the read and settle/world level paths must not claim that
  await assert.rejects(
    gateway("gateway unavailable or online status unknown; no data changed")("/roles/1/modules/monthcard/reset"),
    (error: unknown) => error instanceof ApiError && error.status === 502 && error.message === "网关不可用，未修改"
  );
  await assert.rejects(
    gateway("gateway unavailable")("/servers/s1/worldlev"),
    (error: unknown) => error instanceof ApiError && error.message.includes("核对审计") && !error.message.includes("未修改")
  );
  await assert.rejects(
    gateway("invalid gateway response")("/servers/s1/overview"),
    (error: unknown) => error instanceof ApiError && error.message.includes("网关响应无效")
  );
  // a push (mail, notice, kick) keeps the wording about a push that did not go out, whatever the Go text is
  await assert.rejects(
    gateway("the gateway could not be reached: dial tcp: refused")("/roles/1/kick"),
    (error: unknown) => error instanceof ApiError && error.message === "无法连接游戏网关，本次推送未完成；请检查网关后重试"
  );
  await assert.rejects(
    createApi(session, (async () => new Response("<html>", { status: 502 })) as typeof fetch)("/online"),
    (error: unknown) => error instanceof ApiError && error.message.includes("本次推送未完成")
  );
});
test("401 clears current session; 403 keeps session and conveys Go error; 429 preserves retry", async () => {
  const session = active();
  const forbidden = createApi(session, (async () =>
    Response.json({ error: "your role may not do this" }, { status: 403 })) as typeof fetch);
  await assert.rejects(
    forbidden("/servers"),
    (error: unknown) => error instanceof ApiError && error.status === 403 && error.message.includes("权限")
  );
  assert.ok(session.state.token);
  const locked = createApi(session, (async () =>
    Response.json(
      { error: "too many failed attempts, try again later" },
      { status: 429, headers: { "Retry-After": "900" } }
    )) as typeof fetch);
  await assert.rejects(
    locked("/login", { public: true }),
    (error: unknown) => error instanceof ApiError && error.retryAfter === 900
  );
  assert.ok(session.state.token);
  const denied = createApi(session, (async () =>
    Response.json({ error: "invalid or expired token" }, { status: 401 })) as typeof fetch);
  await assert.rejects(denied("/me"));
  assert.equal(session.state.token, "");
  assert.equal(session.state.admin, null);
});
test("late success/401 from previous login neither populates nor clears a new session", async () => {
  for (const status of [200, 401]) {
    const session = active();
    let resolve!: (response: Response) => void;
    const pending = new Promise<Response>(done => {
      resolve = done;
    });
    const api = createApi(session, (async () => pending) as typeof fetch);
    const result = api("/stats");
    session.clear();
    session.start("new-test-token", future());
    resolve(Response.json(status === 200 ? { accounts: 5 } : { error: "invalid token" }, { status }));
    await assert.rejects(result, StaleSessionError);
    assert.equal(session.state.token, "new-test-token");
  }
});
test("network, timeout and non-JSON failures are actionable", async () => {
  const session = active();
  for (const name of ["TypeError", "TimeoutError"]) {
    const api = createApi(session, (async () => {
      const error = new Error("failure");
      error.name = name;
      throw error;
    }) as typeof fetch);
    await assert.rejects(
      api("/stats"),
      (error: unknown) => error instanceof ApiError && error.status === 0 && /连接|超时/.test(error.message)
    );
  }
  const api = createApi(session, (async () => new Response("<html>not JSON</html>")) as typeof fetch);
  await assert.rejects(api("/stats"), /无法识别/);
});

test("payment conflicts and unsupported activity extensions have actionable Chinese errors", () => {
  assert.match(new ApiError(409, "the order is not waiting for payment any more").message, /不能重复/);
  assert.match(new ApiError(422, 'spec: json: unknown field "future"').message, /未被自动删除/);
  assert.match(new ApiError(422, "these roles do not exist: [999]").message, /收件角色不存在/);
});
