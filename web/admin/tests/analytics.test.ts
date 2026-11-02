import test from "node:test";
import assert from "node:assert/strict";
import {
  duration,
  yuan,
  rate,
  payConversion,
  recorded,
  eventFigure,
  totals,
  trendOption,
  type Analytics,
  type AnalyticsDay
} from "../src/domain/analytics.js";
const day = (d: string, o: Partial<AnalyticsDay> = {}): AnalyticsDay => ({
  day: d,
  new_accounts: 0,
  new_roles: 0,
  active: 0,
  login_accounts: 0,
  sessions: 0,
  avg_session_sec: 0,
  peak_online: 0,
  orders_created: 0,
  orders_paid: 0,
  paying_roles: 0,
  paid_cents: 0,
  ...o
});
test("durations, money and rates read naturally and never invent a figure", () => {
  assert.equal(duration(0), "—");
  assert.equal(duration(-5), "—");
  assert.equal(duration(45), "45 秒");
  assert.equal(duration(600), "10 分 0 秒");
  assert.equal(duration(3 * 3600 + 25 * 60 + 9), "3 小时 25 分");
  assert.equal(yuan(64800), "648");
  assert.equal(yuan(150), "1.5");
  assert.equal(rate(null, 5), "—");
  assert.equal(rate(1, 0), "—");
  assert.equal(rate(0, 4), "0%");
  assert.equal(rate(1, 3), "33%");
  assert.equal(payConversion({ orders_created: 0, orders_paid: 0 }), "—");
  assert.equal(payConversion({ orders_created: 4, orders_paid: 1 }), "25%");
});
test("days before the first recorded event show a dash, not a zero", () => {
  assert.equal(recorded("2026-11-01", ""), false);
  assert.equal(recorded("2026-10-31", "2026-11-01"), false);
  assert.equal(recorded("2026-11-01", "2026-11-01"), true);
  assert.equal(eventFigure(0, "2026-10-31", "2026-11-01"), "—");
  assert.equal(eventFigure(0, "2026-11-01", "2026-11-01"), "0");
  assert.equal(eventFigure(600, "2026-11-01", "2026-11-01", duration), "10 分 0 秒");
});
test("window totals and the trend chart keep unrecorded days empty", () => {
  const days = [day("2026-10-31", { new_roles: 2, paid_cents: 100, orders_paid: 1 }), day("2026-11-01", { new_roles: 1, active: 3, peak_online: 5, paid_cents: 200 })];
  assert.deepEqual(totals(days), { newAccounts: 0, newRoles: 3, paidCents: 300, ordersPaid: 1, peakOnline: 5 });
  const a: Analytics = { from: "2026-10-31", to: "2026-11-01", events_from: "2026-11-01", days, retention: [], cmds: [] };
  const opt = trendOption(a);
  const series = Object.fromEntries(opt.series.map(s => [s.name, s.data]));
  assert.deepEqual(series["日活"], [null, 3]);
  assert.deepEqual(series["新增角色"], [2, 1]);
  assert.deepEqual(opt.xAxis.data, ["10-31", "11-01"]);
});
