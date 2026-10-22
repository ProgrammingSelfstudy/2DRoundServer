import { get, post } from "/api.js";
import { h, modal, badge, table, pager, select, toast, confirmBox } from "/dom.js";
import { GATEWAY_DOWN, buildGrantPayload, buildHeroGrantPayload, fmtTime, grantSummary, heroGrantSummary, pageInfo, queryString } from "/lib.js";
import { attachRows } from "/attach.js";

export async function render(root, ctx) {
  const limit = 25;
  const { items: servers } = await get("/api/servers");
  const q = h("input", { type: "search", placeholder: "角色名、账号名或角色 ID", value: ctx.params.get("q") || "" });
  const srv = select([["", "全部区服"], ...servers.map((s) => [s.srv_id, s.name + "（" + s.srv_id + "）"])], ctx.params.get("srv_id") || "");
  const box = h("div", {});
  let offset = 0;

  async function load() {
    const r = await get("/api/roles" + queryString({ q: q.value.trim(), srv_id: srv.value, limit, offset }));
    const canWrite = ctx.can("write");
    box.replaceChildren(
      table(["角色 ID", "名称", "账号", "区服", "等级", "状态", "创建时间", "最近登录", "操作"], r.items.map((x) => h("tr", {},
        h("td", { text: x.rid }), h("td", { text: x.name }), h("td", { text: x.account }), h("td", { text: x.srv_id }), h("td", { text: x.lev }),
        h("td", {}, x.status === 1 ? badge("正常", "ok") : badge("已封禁", "err")),
        h("td", { text: fmtTime(x.created_at) }), h("td", { text: fmtTime(x.last_login_at) }),
        h("td", { class: "actions" },
          h("button", { class: "btn", type: "button", text: "详情", onclick: () => detail(x) }),
          canWrite ? [
            h("a", { class: "btn", href: "#/mail" + queryString({ to: x.rid }), text: "发邮件" }),
            x.status === 1
              ? h("button", { class: "btn danger", type: "button", text: "封禁", onclick: () => ban(x) })
              : h("button", { class: "btn", type: "button", text: "解封", onclick: () => unban(x) })] : null)))),
      pager(pageInfo(r.total, limit, offset), (o) => { offset = o; load().catch((e) => toast(e.message, "err")); }));
  }

  // detail shows what the game holds for the role: currencies and the item ledger.
  // The ledger also holds items the game's backpack does not list (equipment, ...).
  async function detail(x) {
    let d;
    try { d = await get(`/api/roles/${x.rid}`); } catch (e) { toast(e.message, "err"); return; }
    const online = d.online === true ? badge("在线", "ok") : d.online === false ? badge("离线") : badge("未知（" + GATEWAY_DOWN + "）", "warn");
    const actions = [{ label: "关闭", onclick: (close) => close() }];
    if (ctx.can("write")) actions.unshift({ label: "踢下线", kind: "danger", onclick: (close) => kick(d.role, close) });
    // the client reads the level when it logs in, so an online role sees a change after it reconnects
    const curLev = h("b", { text: String(d.role.lev) });
    const lev = h("input", { type: "number", min: 1, max: 200, value: d.role.lev, class: "inline-num" });
    const kickToo = h("input", { type: "checkbox" });
    const setLev = h("button", { class: "btn", type: "button", text: "设置等级", onclick: async () => {
      try {
        const res = await post(`/api/roles/${d.role.rid}/level`, { lev: Number(lev.value), kick: kickToo.checked });
        curLev.textContent = String(res.lev);
        toast(`等级 ${res.was} → ${res.lev}` + (d.online && !kickToo.checked ? "（在线角色重新登录后生效）" : ""));
        await load();
      } catch (e) { toast(e.message, "err"); }
    } });
    const levRow = ctx.can("write") ? h("p", {}, "等级：", lev, " ", setLev, " ", h("label", {}, kickToo, " 同时踢下线（让客户端重新登录）")) : null;
    // VIP is also read at login; level 0 is no VIP and 30 is the highest the client knows
    const curVip = h("b", { text: String(d.role.vip_lev || 0) });
    const vip = h("input", { type: "number", min: 0, max: 30, value: d.role.vip_lev || 0, class: "inline-num" });
    const kickVip = h("input", { type: "checkbox" });
    const setVip = h("button", { class: "btn", type: "button", text: "设置 VIP", onclick: async () => {
      try {
        const res = await post(`/api/roles/${d.role.rid}/vip`, { vip_lev: Number(vip.value), kick: kickVip.checked });
        curVip.textContent = String(res.vip_lev);
        toast(`VIP ${res.was} → ${res.vip_lev}` + (d.online && !kickVip.checked ? "（在线角色重新登录后生效）" : ""));
        await load();
      } catch (e) { toast(e.message, "err"); }
    } });
    const vipRow = ctx.can("write") ? h("p", {}, "VIP：", vip, " ", setVip, " ", h("label", {}, kickVip, " 同时踢下线（让客户端重新登录）")) : null;
    // a direct grant: currencies and items go straight into the ledgers, no mail
    let grantBox = null;
    if (ctx.can("write")) {
      try {
        const attach = attachRows(await get("/api/meta"));
        attach.add();
        const grantErr = h("p", { class: "form-error", role: "alert" });
        const grant = h("button", { class: "btn primary", type: "button", text: "发放", onclick: async () => {
          grantErr.textContent = "";
          const r = buildGrantPayload(attach.read());
          if (r.error) { grantErr.textContent = r.error; return; }
          try {
            const res = await post(`/api/roles/${d.role.rid}/grant`, r.payload);
            const sum = grantSummary(res);
            toast(sum.text, sum.kind);
            shown.close();
            await detail(x);
          } catch (e) { grantErr.textContent = e.message; }
        } });
        grantBox = h("div", {}, h("h4", { text: "发放资源" }),
          h("p", { class: "hint", text: "直接进入玩家资产或背包，不走邮件。物品只有「道具」类会显示在背包里。" }),
          attach.box, h("button", { class: "btn", type: "button", text: "添加一项", onclick: () => attach.add() }), " ", grant, grantErr);
      } catch (e) { toast(e.message, "err"); }
    }
    // heroes are made like summoned ones: level 1, the star chosen here
    let heroBox = null;
    if (ctx.can("write")) {
      try {
        const meta = await get("/api/meta");
        const camps = { 1: "水", 2: "火", 3: "风", 4: "光", 5: "暗" };
        const hero = select(meta.heroes.map((x) => [x.bid, `${x.name}（${x.bid}，${camps[x.camp] || "-"}，${x.init_star}星起）`]), meta.heroes[0] && meta.heroes[0].bid);
        const star = h("input", { type: "number", min: 1, max: 15, value: 5, class: "inline-num" });
        const num = h("input", { type: "number", min: 1, max: 20, value: 1, class: "inline-num" });
        const heroErr = h("p", { class: "form-error", role: "alert" });
        const giveHero = h("button", { class: "btn primary", type: "button", text: "发放英雄", onclick: async () => {
          heroErr.textContent = "";
          const r = buildHeroGrantPayload([{ bid: hero.value, star: star.value, num: num.value }], meta.heroes);
          if (r.error) { heroErr.textContent = r.error; return; }
          try {
            const res = await post(`/api/roles/${d.role.rid}/heroes`, r.payload);
            const sum = heroGrantSummary(res);
            toast(sum.text, sum.kind);
          } catch (e) { heroErr.textContent = e.message; }
        } });
        const setStar = () => { const x = meta.heroes.find((y) => String(y.bid) === hero.value); if (x) { star.value = x.init_star; star.min = x.init_star; star.max = x.max_star; } };
        hero.addEventListener("change", setStar);
        setStar();
        heroBox = h("div", {}, h("h4", { text: "发放英雄" }),
          h("p", { class: "hint", text: "直接给角色新英雄（1 级，指定星级），不占邮件。不检查英雄格子上限。" }),
          h("p", {}, hero, " 星级 ", star, " 数量 ", num, " ", giveHero), heroErr);
      } catch (e) { toast(e.message, "err"); }
    }
    const shown = modal({
      title: `角色「${d.role.name}」（${d.role.rid}）`, wide: true, actions,
      body: h("div", {},
        h("p", {}, "账号：", d.account, "　区服：", d.role.srv_id, "　等级：", curLev, "　VIP：", curVip, "　", online, d.banned ? badge("已封禁", "err") : null),
        levRow,
        vipRow,
        grantBox,
        heroBox,
        h("h4", { text: "资源" }),
        table(["资源", "名称", "数量"], d.assets.map((a) => h("tr", {}, h("td", { text: a.label }), h("td", { text: a.name || "—" }), h("td", { text: a.val })))),
        h("h4", { text: "物品" }),
        table(["物品 ID", "名称", "数量", "游戏背包"], d.items.map((i) => h("tr", {}, h("td", { text: i.base_id }), h("td", { text: i.name || "—" }), h("td", { text: i.num }),
          h("td", {}, i.listed ? badge("显示", "ok") : badge("不显示")))))),
    });
  }

  async function kick(role, close) {
    try {
      const res = await post(`/api/roles/${role.rid}/kick`);
      toast(res.kicked ? "已踢下线" : "该角色当前不在线");
      close();
    } catch (e) { toast(e.status === 502 ? GATEWAY_DOWN : e.message, "err"); }
  }

  async function ban(x) {
    if (!(await confirmBox(`封禁角色「${x.name}」（${x.rid}）？在线的话会被立即踢下线。`, { danger: true, confirm: "封禁" }))) return;
    try { await post(`/api/roles/${x.rid}/ban`); toast("已封禁"); await load(); } catch (e) { toast(e.message, "err"); }
  }
  async function unban(x) {
    try { await post(`/api/roles/${x.rid}/unban`); toast("已解封"); await load(); } catch (e) { toast(e.message, "err"); }
  }

  const go = () => { offset = 0; load().catch((e) => toast(e.message, "err")); };
  q.addEventListener("keydown", (e) => e.key === "Enter" && go());
  srv.addEventListener("change", go);
  root.append(h("div", { class: "toolbar" }, q, srv, h("button", { class: "btn", type: "button", text: "搜索", onclick: go })), box);
  await load();
}
