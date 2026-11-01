// Read-only smoke check for the embedded Go UI (or an explicitly selected Vite URL).
import assert from "node:assert/strict";
const base = process.env.ADMIN_UI_URL || "http://127.0.0.1:19080";
async function get(path) {
  return fetch(new URL(path, base), { signal: AbortSignal.timeout(5000), redirect: "error" });
}
const entry = await get("/");
assert.equal(entry.status, 200, "frontend entry");
const html = await entry.text();
assert.match(html, /id="app"/, "Vue mounting element");
const modulePath = html.match(/<script[^>]*type="module"[^>]*src="([^"]+)"/)?.[1];
assert.ok(modulePath, "module entry exists");
const module = await get(modulePath);
assert.equal(module.status, 200, "module entry served");
assert.match(module.headers.get("content-type"), /javascript/, "module MIME type");
const health = await get("/api/healthz");
assert.equal(health.status, 200, "Go health endpoint (preserves /api)");
assert.deepEqual(await health.json(), { ok: true });
const me = await get("/api/me");
assert.equal(me.status, 401, "Go authorization remains enforced through proxy");
assert.equal(typeof (await me.json()).error, "string", "Go error format is preserved");
const missing = await get("/api/nonexistent-environment-smoke");
assert.equal(missing.status, 404, "unknown API must not fall back to HTML");
console.log(`PASS ${base}: HTML, module, Go health, 401 authorization, API 404`);
