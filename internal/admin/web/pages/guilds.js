import { get, post } from "/api.js";
import { h, field, select, table, pager, toast, confirmBox } from "/dom.js";
import { fmtTime, pageInfo } from "/lib.js";

export async function render(root, ctx) {
  const limit = 25;
  const { items: servers } = await get("/api/servers");
  const box = h("div", {});
  const canWrite = ctx.can("write");
  let offset = 0, srv = "", q = "";

  async function load() {
    const r = await get(`/api/guilds?limit=${limit}&offset=${offset}&srv_id=${encodeURIComponent(srv)}&q=${encodeURIComponent(q)}`);
    box.replaceChildren(table(["ID", "区服", "名称", "等级", "人数", "会长", "会长编号", "创建", canWrite ? "操作" : ""].filter(Boolean), r.items.map((g) => h("tr", {},
      h("td", { text: g.gid }), h("td", { text: g.srv_id }), h("td", { text: g.name }), h("td", { text: g.lev }), h("td", { text: g.members }), h("td", { text: g.leader }),
      h("td", { text: g.leader_rid }), h("td", { text: fmtTime(g.created_at) }),
      canWrite ? h("td", { class: "actions" }, h("button", { class: "btn danger", type: "button", text: "解散", onclick: () => disband(g) })) : null))),
      pager(pageInfo(r.total, limit, offset), (o) => { offset = o; load().catch((e) => toast(e.message, "err")); }));
  }

  async function disband(g) {
    if (!(await confirmBox(`解散公会「${g.name}」？成员、申请和日志都会删除，会长要等冷却才能再建公会。在线成员重新登录后才会发现。`, { danger: true, confirm: "解散" }))) return;
    try { await post(`/api/guilds/${g.gid}/disband`, {}); toast("已解散"); await load(); } catch (e) { toast(e.message, "err"); }
  }

  const sel = select([["", "全部区服"], ...servers.map((s) => [s.srv_id, s.name + "（" + s.srv_id + "）"])], "");
  const name = h("input", { type: "search", placeholder: "公会名称" });
  const go = () => { srv = sel.value; q = name.value.trim(); offset = 0; load().catch((e) => toast(e.message, "err")); };
  sel.addEventListener("change", go);
  name.addEventListener("keydown", (e) => { if (e.key === "Enter") go(); });
  root.append(h("div", { class: "toolbar" }, sel, name, h("button", { class: "btn", type: "button", text: "搜索", onclick: go })), box);
  await load();
}
