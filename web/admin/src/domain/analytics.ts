export interface AnalyticsDay {
  day: string;
  new_accounts: number;
  new_roles: number;
  active: number;
  login_accounts: number;
  sessions: number;
  avg_session_sec: number;
  peak_online: number;
  orders_created: number;
  orders_paid: number;
  paying_roles: number;
  paid_cents: number;
}
export interface AnalyticsRetention {
  day: string;
  cohort: number;
  d1: number | null;
  d3: number | null;
  d7: number | null;
}
export interface AnalyticsCmd {
  cmd: number;
  hits: number;
  roles: number;
}
export interface Analytics {
  from: string;
  to: string;
  events_from: string;
  days: AnalyticsDay[];
  retention: AnalyticsRetention[];
  cmds: AnalyticsCmd[];
}
export const analyticsWindows = [7, 14, 30, 90] as const;

export function duration(sec: number) {
  if (!(sec > 0)) return "—";
  if (sec < 60) return `${sec} 秒`;
  if (sec < 3600) return `${Math.floor(sec / 60)} 分 ${sec % 60} 秒`;
  return `${Math.floor(sec / 3600)} 小时 ${Math.floor((sec % 3600) / 60)} 分`;
}
export function yuan(cents: number) {
  return (cents / 100).toLocaleString("zh-CN", { maximumFractionDigits: 2 });
}
/** Share of a day's new roles that came back, as a percentage; "—" while unknown (kept is null) or when nobody was created. */
export function rate(kept: number | null, cohort: number) {
  return kept == null || cohort <= 0 ? "—" : `${Math.round((kept / cohort) * 100)}%`;
}
/** Paid orders over created orders of the day. */
export function payConversion(day: Pick<AnalyticsDay, "orders_created" | "orders_paid">) {
  return day.orders_created > 0 ? `${Math.round((day.orders_paid / day.orders_created) * 100)}%` : "—";
}
/** Events are recorded from events_from on; earlier days have no figure for them, which is not the same as zero. */
export function recorded(day: string, eventsFrom: string) {
  return eventsFrom !== "" && day >= eventsFrom;
}
export function eventFigure(value: number, day: string, eventsFrom: string, format: (n: number) => string = String) {
  return recorded(day, eventsFrom) ? format(value) : "—";
}
export function totals(days: AnalyticsDay[]) {
  const sum = (pick: (d: AnalyticsDay) => number) => days.reduce((n, d) => n + pick(d), 0);
  return {
    newAccounts: sum(d => d.new_accounts),
    newRoles: sum(d => d.new_roles),
    paidCents: sum(d => d.paid_cents),
    ordersPaid: sum(d => d.orders_paid),
    peakOnline: days.reduce((n, d) => Math.max(n, d.peak_online), 0)
  };
}
/** Line chart of the daily figures; days before the first recorded event are left out of the event lines. */
export function trendOption(a: Analytics) {
  const days = a.days;
  const line = (name: string, pick: (d: AnalyticsDay) => number, events: boolean) => ({
    name,
    type: "line",
    smooth: true,
    data: days.map(d => (events && !recorded(d.day, a.events_from) ? null : pick(d)))
  });
  return {
    tooltip: { trigger: "axis" },
    legend: { data: ["日活", "新增角色", "新增账号", "在线峰值"] },
    grid: { left: 40, right: 20, top: 40, bottom: 30 },
    xAxis: { type: "category", data: days.map(d => d.day.slice(5)) },
    yAxis: { type: "value", minInterval: 1 },
    series: [
      line("日活", d => d.active, true),
      line("新增角色", d => d.new_roles, false),
      line("新增账号", d => d.new_accounts, false),
      line("在线峰值", d => d.peak_online, true)
    ]
  };
}
