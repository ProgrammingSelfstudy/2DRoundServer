// Exercises only accounts/roles in an explicitly marked disposable sandbox.
// Credentials/tokens are never printed. Start with ADMIN_SANDBOX_FIXTURES=1.
import assert from "node:assert/strict";
import { readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { randomBytes } from "node:crypto";
const file = process.env.ADMIN_UI_FIXTURE;
assert.ok(file, "Set ADMIN_UI_FIXTURE to the sandbox fixture.json path");
assert.equal(await readFile(join(dirname(file), ".admin-ui-sandbox"), "utf8"), "2dround-admin-sandbox\n");
const fixture = JSON.parse(await readFile(file, "utf8"));
const base = new URL(fixture.api);
assert.equal(base.hostname, "127.0.0.1", "sandbox must be loopback");
assert.ok(fixture.users.viewer.username.startsWith("ui_"), "sandbox admin required");
const tokens = {};
async function call(method, path, body, token, status = 200) {
  const headers = { Accept: "application/json" };
  if (token) headers.Authorization = `Bearer ${token}`;
  if (body !== undefined) headers["Content-Type"] = "application/json";
  const response = await fetch(new URL(path, base), { method, headers, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(10000), redirect: "error" });
  assert.equal(response.status, status, `${method} ${path}: HTTP status`);
  return { body: await response.json(), retryAfter: Number(response.headers.get("Retry-After")) };
}
await call("POST", "/api/login", { username: fixture.users.viewer.username, password: "deliberately-invalid-test-password" }, null, 401);
for (const role of ["viewer", "operator", "superadmin"]) {
  const { username, password } = fixture.users[role];
  const { body } = await call("POST", "/api/login", { username, password });
  assert.ok(typeof body.token === "string", `${role} login`); tokens[role] = body.token;
  const me = (await call("GET", "/api/me", undefined, tokens[role])).body;
  assert.equal(me.role, role);
  for (const path of ["/api/stats", "/api/servers", "/api/roles?limit=25&offset=0"]) await call("GET", path, undefined, tokens[role]);
}
const superToken = tokens.superadmin; const operator = tokens.operator; const viewer = tokens.viewer;
await call("GET", "/api/me", undefined, "invalid-test-token", 401);
for (const path of ["/api/admins", "/api/audit", "/api/backups"]) await call("GET", path, undefined, viewer, 403);
const rid = fixture.rid;
for (const path of ["level", "vip", "grant", "heroes", "skins", "looks", "ban", "unban", "kick"]) {
  await call("POST", `/api/roles/${rid}/${path}`, {}, viewer, 403);
}
await call("POST", "/api/broadcast", { msg: "test" }, viewer, 403);
await call("POST", "/api/servers", {}, operator, 403);
await call("PUT", "/api/servers/uitest_1", {}, operator, 403);
await call("DELETE", "/api/servers/uitest_1", undefined, operator, 403);
const page1 = (await call("GET", "/api/roles?limit=25&offset=0", undefined, viewer)).body;
const page2 = (await call("GET", "/api/roles?limit=25&offset=25", undefined, viewer)).body;
assert.equal(page1.total, 31); assert.equal(page1.items.length, 25); assert.equal(page2.items.length, 6);
assert.ok(!page1.items.some(a => page2.items.some(b => a.rid === b.rid)), "pages do not overlap");
for (const q of ["测试角色01", "ui_account_01", String(rid)]) {
  const rows = (await call("GET", `/api/roles?q=${encodeURIComponent(q)}`, undefined, viewer)).body.items;
  assert.ok(rows.some(r => r.rid === rid), "search finds seeded role");
}
assert.equal((await call("GET", "/api/roles?srv_id=uitest_2", undefined, viewer)).body.total, 3);
const server = { srv_id: "uismoke_9999", name: "冒烟测试服", platform: "uitest", zone_id: 99, host: "127.0.0.1", port: 18999, ws_url: "", status: 1, is_new: true, is_recommend: false, open_time: 0, main_srv_id: "", group_id: 1 };
await call("POST", "/api/servers", server, superToken, 201);
for (const status of [0, 1]) {
  const result = (await call("PUT", `/api/servers/${server.srv_id}`, { ...server, status, name: "冒烟已编辑" }, superToken)).body;
  assert.equal(result.status, status); assert.equal(result.name, "冒烟已编辑");
}
await call("DELETE", `/api/servers/${server.srv_id}`, undefined, superToken);
await call("DELETE", "/api/servers/uitest_1", undefined, superToken, 409);
const before = (await call("GET", `/api/roles/${rid}`, undefined, viewer)).body;
assert.equal(before.online, null); assert.ok(before.online_error, "unavailable gateway is explicit");
await call("POST", `/api/roles/${rid}/level`, { lev: 42 }, operator);
await call("POST", `/api/roles/${rid}/vip`, { vip_lev: 6 }, operator);
await call("POST", `/api/roles/${rid}/level`, { lev: 201 }, operator, 422);
const meta = (await call("GET", "/api/meta", undefined, viewer)).body;
const items = (await call("GET", `/api/items?q=${encodeURIComponent("生命金卡")}`, undefined, viewer)).body.items;
const item = items.find(i => !i.currency); assert.ok(item, "named item search works");
const grant = (await call("POST", `/api/roles/${rid}/grant`, { grants: [{ kind: "asset", asset: "gold", num: 100 }, { kind: "item", item_id: item.base_id, num: 2 }] }, operator)).body;
assert.equal(grant.refreshed, null); assert.ok(grant.push_error, "grant saves despite gateway being unavailable");
const hero = meta.heroes[0]; assert.ok(hero, "hero metadata exists");
const heroes = (await call("POST", `/api/roles/${rid}/heroes`, { heroes: [{ bid: hero.bid, star: hero.init_star, num: 1 }] }, operator)).body;
assert.equal(heroes.granted.length, 1); assert.ok(heroes.push_error);
const skin = meta.skins[0]; assert.ok(skin, "skin metadata exists");
const skinReply = (await call("POST", `/api/roles/${rid}/skins`, { skin_id: skin.id, days: 0 }, operator)).body; assert.ok(skinReply.push_error);
const look = meta.looks.find(l => l.kind === "avatar"); assert.ok(look, "appearance metadata exists");
await call("POST", `/api/roles/${rid}/looks`, { kind: look.kind, base_id: look.id, days: 7 }, operator);
const after = (await call("GET", `/api/roles/${rid}`, undefined, viewer)).body;
assert.equal(after.role.lev, 42); assert.equal(after.role.vip_lev, 6);
const assetValue = d => d.assets.find(a => a.label === 3)?.val || 0;
assert.equal(assetValue(after) - assetValue(before), 100);
const itemValue = d => d.items.find(i => i.base_id === item.base_id)?.num || 0;
assert.equal(itemValue(after) - itemValue(before), 2);
await call("POST", `/api/roles/${rid}/ban`, {}, operator);
assert.equal((await call("GET", `/api/roles/${rid}`, undefined, viewer)).body.banned, true);
await call("POST", `/api/roles/${rid}/unban`, {}, operator);
assert.equal((await call("GET", `/api/roles/${rid}`, undefined, viewer)).body.banned, false);
await call("POST", `/api/roles/${rid}/kick`, {}, operator, 502);
await call("POST", "/api/broadcast", { msg: "后台隔离测试广播" }, operator, 502);
await call("POST", "/api/broadcast", { msg: "" }, operator, 422);
await call("PATCH", `/api/admins/${fixture.users.operator.id}`, { role: "viewer" }, superToken);
assert.equal((await call("GET", "/api/me", undefined, operator)).body.role, "viewer");
await call("POST", `/api/roles/${rid}/level`, { lev: 43 }, operator, 403);
await call("PATCH", `/api/admins/${fixture.users.operator.id}`, { role: "operator" }, superToken);
await call("PATCH", `/api/admins/${fixture.users.viewer.id}`, { status: 0 }, superToken);
await call("GET", "/api/me", undefined, viewer, 401);
await call("PATCH", `/api/admins/${fixture.users.viewer.id}`, { status: 1 }, superToken);
// Password reset invalidates the browser's existing viewer session, for UI verification.
fixture.users.viewer.password = randomBytes(18).toString("hex");
await call("PATCH", `/api/admins/${fixture.users.viewer.id}`, { password: fixture.users.viewer.password }, superToken);
await writeFile(file, JSON.stringify(fixture), { mode: 0o600 });
await call("GET", "/api/me", undefined, viewer, 401);
const lockUser = { username: `ui_lock_${randomBytes(4).toString("hex")}`, password: randomBytes(18).toString("hex"), role: "viewer" };
await call("POST", "/api/admins", lockUser, superToken, 201);
for (let i = 0; i < 5; i++) await call("POST", "/api/login", { username: lockUser.username, password: "invalid" }, null, 401);
const locked = await call("POST", "/api/login", { username: lockUser.username, password: "invalid" }, null, 429);
assert.ok(locked.retryAfter > 0);
console.log("PASS: real Go login/failure/429, three roles, paging/search/filter, server CRUD/conflict, role level/VIP/grants/heroes/skin/look/ban, gateway failure, demotion/disable/password invalidation");
