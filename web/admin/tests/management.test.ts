import test from "node:test";
import assert from "node:assert/strict";
import {
  managementPages,
  pageCapability,
  actionAllowed,
  password,
  adminPayload,
  ownPassword,
  banReason,
  ticketAnswer,
  redeemPayload,
  noticePayload,
  noticeFeedback
} from "../src/domain/management.js";
import { can } from "../src/auth/permissions.js";
import { AdminSession } from "../src/auth/session.js";
import { createApi, ApiError } from "../src/api/client.js";
test("eleven remaining pages retain exact server read/write/manage contract, unknown fails closed", () => {
  assert.equal(managementPages.length, 11);
  for (const role of ["viewer", "operator", "superadmin"])
    for (const [page, , read, write] of managementPages) {
      assert.equal(can(role, pageCapability(page)), read === "read" || role === "superadmin");
      assert.equal(
        actionAllowed(role, page),
        write === "read" || (write === "write" && role !== "viewer") || (write === "manage" && role === "superadmin")
      );
    }
  assert.equal(actionAllowed("superadmin", "reports"), false);
  assert.equal(actionAllowed("superadmin", "missing"), false);
  assert.equal(pageCapability("missing"), undefined);
  assert.equal(actionAllowed("constructor", "admins"), false);
});
test("password limits match bcrypt UTF-8 bytes; whitespace not silently trimmed", () => {
  assert.equal(password("  abcdef"), "  abcdef");
  assert.equal(password("汉".repeat(24)).length, 24);
  assert.throws(() => password("汉".repeat(25)));
  assert.throws(() => password("1234567"));
  assert.equal(password("x".repeat(72)).length, 72);
  assert.throws(() => password("x".repeat(73)));
  assert.throws(() => ownPassword("", "12345678", "12345678"));
  assert.throws(() => ownPassword("old", "12345678", "12345679"));
  assert.deepEqual(ownPassword("old", "12345678", "12345678"), { old: "old", new: "12345678" });
});
test("admin creation validates names and roles and never mixes game account fields", () => {
  assert.deepEqual(Object.keys(adminPayload("admin.1", "12345678", "viewer")), ["username", "password", "role"]);
  for (const name of ["ab", "abc汉", "a b", "x".repeat(33)]) assert.throws(() => adminPayload(name, "12345678", "operator"));
  assert.throws(() => adminPayload("okay", "12345678", "root" as any));
});
test("account reasons and ticket replies use Unicode character counts and preserve content", () => {
  assert.deepEqual(banReason(""), { reason: "" });
  assert.doesNotThrow(() => banReason("😀".repeat(200)));
  assert.throws(() => banReason("😀".repeat(201)));
  assert.throws(() => ticketAnswer("   "));
  assert.throws(() => ticketAnswer("汉".repeat(1001)));
  assert.equal(ticketAnswer("  回复\n正文  ").content, "  回复\n正文  ");
});
test("redeem rewards/limits/expiry exact request and UTF-8 note boundary", () => {
  const form = { code: " abcD ", note: "测试", max_uses: 0, days: 0 },
    row = { key: 1, kind: "asset" as const, asset: "gold", num: 1 };
  assert.deepEqual(redeemPayload(form, [row], [{ name: "gold", label: 3 }]), {
    code: "abcD",
    note: "测试",
    max_uses: 0,
    days: 0,
    grants: [{ kind: "asset", asset: "gold", num: 1 }]
  });
  assert.throws(() => redeemPayload(form, [], []));
  for (const f of [
    { ...form, max_uses: 1000001 },
    { ...form, days: 3651 },
    { ...form, max_uses: 0.5 },
    { ...form, code: "汉字码" },
    { ...form, note: "汉".repeat(67) }
  ])
    assert.throws(() => redeemPayload(f, [row], [{ name: "gold", label: 3 }]));
  assert.doesNotThrow(() =>
    redeemPayload({ ...form, days: 3650, max_uses: 1000000 }, Array(20).fill(row), [{ name: "gold", label: 3 }])
  );
  assert.throws(() => redeemPayload(form, Array(21).fill(row), [{ name: "gold", label: 3 }]));
});
test("notice blank dates preserve 0, local timestamps and status exact, invalid calendar/range rejected", () => {
  const f = { title: "公告", content: "正文\n第二行", srv_id: "", start: "", end: "", shown: true };
  assert.deepEqual(noticePayload(f), { title: "公告", content: "正文\n第二行", srv_id: "", start_at: 0, end_at: 0, status: 1 });
  const r = noticePayload({ ...f, start: "2026-11-01T12:00:12", end: "2026-11-01T12:00:13", shown: false });
  assert.equal(r.end_at - r.start_at, 1);
  assert.equal(r.status, 0);
  assert.throws(() => noticePayload({ ...f, start: "2026-02-30T12:00" }));
  assert.throws(() => noticePayload({ ...f, start: "2026-11-01T12:00", end: "2026-11-01T11:59" }));
  assert.throws(() => noticePayload({ ...f, title: " " }));
  assert.throws(() => noticePayload({ ...f, content: "x".repeat(2001) }));
});
test("notice stored/delete feedback never claims notification or device receipt without evidence", () => {
  assert.equal(noticeFeedback({ pushed: null, push_error: "no gateway configured" }).type, "warning");
  assert.match(noticeFeedback({ pushed: null }, true).message, /已删除/);
  assert.match(noticeFeedback({ pushed: 0 }).message, /0 个/);
  assert.match(noticeFeedback({ pushed: 2 }).message, /未验证真机/);
});
test("wrong own password 403 retains current session, invalidated token 401 clears session", async () => {
  const session = new AdminSession(undefined, () => 0);
  session.start("opaque", 100);
  session.confirm({ id: 1, username: "test", role: "operator", status: 1 }, session.state.revision);
  const denied = createApi(
    session,
    async () => new Response(JSON.stringify({ error: "the current password is wrong" }), { status: 403 })
  );
  await assert.rejects(
    denied("/me/password", { method: "POST", body: { old: "bad", new: "12345678" } }),
    (e: ApiError) => e.message === "当前密码不正确"
  );
  assert.equal(session.state.token, "opaque");
  const expired = createApi(
    session,
    async () => new Response(JSON.stringify({ error: "invalid or expired token" }), { status: 401 })
  );
  await assert.rejects(expired("/me"));
  assert.equal(session.state.token, "");
  assert.equal(session.state.admin, null);
});
