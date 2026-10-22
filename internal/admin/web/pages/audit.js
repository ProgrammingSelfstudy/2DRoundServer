import { get } from "/api.js";
import { h, table, pager, toast } from "/dom.js";
import { fmtTime, pageInfo, truncate } from "/lib.js";

export async function render(root) {
  const limit = 30;
  let offset = 0;
  const box = h("div", {});
  async function load() {
    const r = await get(`/api/audit?limit=${limit}&offset=${offset}`);
    box.replaceChildren(
      table(["时间", "管理员", "操作", "对象", "IP", "详情"], r.items.map((a) => h("tr", {},
        h("td", { text: fmtTime(a.created_at) }), h("td", { text: a.admin_name }), h("td", {}, h("code", { text: a.action })),
        h("td", { text: a.target }), h("td", { text: a.ip }), h("td", { class: "detail", title: a.detail, text: truncate(a.detail, 90) })))),
      pager(pageInfo(r.total, limit, offset), (o) => { offset = o; load().catch((e) => toast(e.message, "err")); }));
  }
  root.append(box);
  await load();
}
