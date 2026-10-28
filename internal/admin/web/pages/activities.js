import { get, post, put, del } from "/api.js";
import { h, modal, field, select, badge, table, pager, toast, confirmBox } from "/dom.js";
import { aimsToText, buildActivityPayload, fmtTime, pageInfo, toLocalInput } from "/lib.js";

export async function render(root, ctx) {
  const limit = 25;
  const { items: servers } = await get("/api/servers");
  const box = h("div", {});
  const canWrite = ctx.can("write");
  let offset = 0;
  let srvFilter = "";

  function state(a) {
    const now = Math.floor(Date.now() / 1000);
    if (a.status !== 1) return badge("关闭");
    if (a.start_at > now) return badge("未开始", "warn");
    if (a.end_at && a.end_at <= now) return badge("已结束");
    return badge("进行中", "ok");
  }

  async function load() {
    const r = await get(`/api/activities?limit=${limit}&offset=${offset}` + (srvFilter ? `&srv_id=${encodeURIComponent(srvFilter)}` : ""));
    box.replaceChildren(
      table(["ID", "区服", "活动号", "页签", "标题", "面板", "开始", "结束", "状态", canWrite ? "操作" : ""].filter(Boolean), r.items.map((a) => h("tr", {},
        h("td", { text: a.id }), h("td", { text: a.srv_id || "全服" }), h("td", { text: a.camp_id }), h("td", { text: a.bid }), h("td", { text: a.spec.title }),
        h("td", { text: a.spec.panel_type }), h("td", { text: fmtTime(a.start_at) }), h("td", { text: a.end_at ? fmtTime(a.end_at) : "不结束" }), h("td", {}, state(a)),
        canWrite ? h("td", { class: "actions" },
          h("button", { class: "btn", type: "button", text: "编辑", onclick: () => edit(a) }),
          h("button", { class: "btn", type: "button", text: "复制", onclick: () => edit(a, true) }),
          h("button", { class: "btn danger", type: "button", text: "删除", onclick: () => remove(a) })) : null))),
      pager(pageInfo(r.total, limit, offset), (o) => { offset = o; load().catch((e) => toast(e.message, "err")); }));
  }

  async function remove(a) {
    if (!(await confirmBox(`删除活动「${a.spec.title}」？玩家已有的进度不会删除。`, { danger: true, confirm: "删除" }))) return;
    try { await del(`/api/activities/${a.id}`); toast("已删除"); await load(); } catch (e) { toast(e.message, "err"); }
  }

  function edit(a, copy = false) {
    const now = Math.floor(Date.now() / 1000);
    const v = a || { srv_id: "", camp_id: "", bid: "", status: 1, start_at: now, end_at: 0, spec: { title: "", panel_type: 16 } };
    const sp = v.spec || {};
    const keep = { ...sp };
    for (const k of ["title", "panel_type", "title2", "rule_str", "time_str", "aim_title", "cli_type_name", "sort_val", "cli_type", "daily", "aims"]) delete keep[k];
    const f = {
      srvID: select([["", "全服"], ...servers.map((s) => [s.srv_id, s.name + "（" + s.srv_id + "）"])], v.srv_id),
      campID: h("input", { type: "number", min: 1, value: v.camp_id }),
      bid: h("input", { type: "number", min: 1, value: v.bid }),
      title: h("input", { value: sp.title || "", maxlength: 40 }),
      title2: h("input", { value: sp.title2 || "" }),
      panelType: h("input", { type: "number", min: 1, max: 145, value: sp.panel_type || 16 }),
      sortVal: h("input", { type: "number", value: sp.sort_val || 0 }),
      cliType: h("input", { type: "number", value: sp.cli_type || 0 }),
      cliTypeName: h("input", { value: sp.cli_type_name || "" }),
      ruleStr: h("textarea", { rows: 3, value: sp.rule_str || "" }),
      timeStr: h("input", { value: sp.time_str || "" }),
      aimTitle: h("input", { value: sp.aim_title || "" }),
      daily: h("input", { type: "checkbox", checked: !!sp.daily }),
      status: h("input", { type: "checkbox", checked: v.status === 1 }),
      startAt: h("input", { type: "datetime-local", value: toLocalInput(v.start_at) }),
      endAt: h("input", { type: "datetime-local", value: toLocalInput(v.end_at) }),
      aims: h("textarea", { rows: 8, spellcheck: false, value: aimsToText(sp.aims), placeholder: "1 | 每日登录 | evt_cli_loaded | 1 | 3:100,10403:2" }),
    };
    const err = h("p", { class: "form-error", role: "alert" });
    const isNew = !a || copy;
    modal({
      title: isNew ? "新建活动" : "编辑活动", wide: true,
      body: h("div", { class: "form-grid" },
        field("区服", f.srvID), field("活动号 camp_id", f.campID, "客户端的活动编号；有专门玩法的活动用约定好的号（见文档），自己做的目标列表随便取一个正数"),
        field("页签 bid", f.bid, "同一个页签号同时只显示最早开始的那个"), field("标题", f.title), field("副标题", f.title2),
        field("面板类型 panel_type", f.panelType, "1~145，目标列表用 2、3、4、15、16"), field("排序", f.sortVal), field("主界面图标 cli_type", f.cliType, "0 = 不挂图标"),
        field("图标名称", f.cliTypeName), field("规则说明", f.ruleStr), field("时间说明", f.timeStr), field("目标标题", f.aimTitle),
        field("开始时间", f.startAt), field("结束时间", f.endAt, "留空 = 不结束"),
        h("label", { class: "field check" }, f.daily, " 每天重新计数"), h("label", { class: "field check" }, f.status, " 开启"),
        field("目标（每行一个）", f.aims, "格式：编号 | 说明 | 事件 | 目标数 | 物品编号:数量,物品编号:数量；事件留空 = 直接可领；可用事件 evt_cli_loaded、evt_recruit、evt_friend_present 等"), err),
      actions: [{ label: "取消", onclick: (c) => c() }, {
        label: "保存", kind: "primary", onclick: async (close) => {
          const r = buildActivityPayload({
            srvID: f.srvID.value, campID: f.campID.value, bid: f.bid.value, title: f.title.value, title2: f.title2.value, panelType: f.panelType.value,
            sortVal: f.sortVal.value, cliType: f.cliType.value, cli_type_name: f.cliTypeName.value, rule_str: f.ruleStr.value, time_str: f.timeStr.value,
            aim_title: f.aimTitle.value, daily: f.daily.checked, status: f.status.checked, startAt: f.startAt.value, endAt: f.endAt.value, aims: f.aims.value,
          }, keep);
          if (r.error) { err.textContent = r.error; return; }
          try { if (isNew) await post("/api/activities", r.payload); else await put(`/api/activities/${a.id}`, r.payload); close(); toast("已保存，网关 15 秒内会通知在线玩家"); await load(); } catch (e) { err.textContent = e.message; }
        },
      }],
    });
  }

  const filter = select([["", "全部区服"], ...servers.map((s) => [s.srv_id, s.name + "（" + s.srv_id + "）"])], "");
  filter.addEventListener("change", () => { srvFilter = filter.value; offset = 0; load().catch((e) => toast(e.message, "err")); });
  root.append(
    h("div", { class: "toolbar" }, filter, canWrite ? h("button", { class: "btn primary", type: "button", text: "新建活动", onclick: () => edit(null) }) : null),
    h("p", { class: "hint", text: "运营在这里开活动：设好活动号、页签、起止时间和面板，保存后网关 15 秒内通知在线玩家重新取列表。带专门玩法的活动（转盘、年兽、烟花等）用约定的活动号，面板类型按客户端的 ActionPanelTypeView；目标列表类面板可以直接在下面写目标和奖励。" }), box);
  await load();
}
