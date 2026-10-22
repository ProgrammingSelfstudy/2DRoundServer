import { get, post, put, del } from "/api.js";
import { h, modal, field, select, badge, table, pager, toast, confirmBox } from "/dom.js";
import { buildNoticePayload, fmtTime, pageInfo, toLocalInput, truncate, pushSummary } from "/lib.js";

export async function render(root, ctx) {
  const limit = 25;
  const { items: servers } = await get("/api/servers");
  const box = h("div", {});
  const canWrite = ctx.can("write");
  let offset = 0;

  async function load() {
    const r = await get(`/api/notices?limit=${limit}&offset=${offset}`);
    box.replaceChildren(
      table(["ID", "标题", "内容", "区服", "开始", "结束", "状态", "发布人", canWrite ? "操作" : ""].filter(Boolean), r.items.map((n) => h("tr", {},
        h("td", { text: n.id }), h("td", { text: n.title }), h("td", { text: truncate(n.content, 40) }), h("td", { text: n.srv_id || "全服" }),
        h("td", { text: fmtTime(n.start_at) }), h("td", { text: n.end_at ? fmtTime(n.end_at) : "不结束" }),
        h("td", {}, n.status === 1 ? badge("显示", "ok") : badge("隐藏")), h("td", { text: n.created_by }),
        canWrite ? h("td", { class: "actions" },
          h("button", { class: "btn", type: "button", text: "编辑", onclick: () => edit(n) }),
          h("button", { class: "btn danger", type: "button", text: "删除", onclick: () => remove(n) })) : null))),
      pager(pageInfo(r.total, limit, offset), (o) => { offset = o; load().catch((e) => toast(e.message, "err")); }));
  }

  async function remove(n) {
    if (!(await confirmBox(`删除公告「${n.title}」？`, { danger: true, confirm: "删除" }))) return;
    try { const res = await del(`/api/notices/${n.id}`); const m = pushSummary(res, "已删除"); toast(m.text, m.kind); await load(); } catch (e) { toast(e.message, "err"); }
  }

  function edit(n) {
    const v = n || { srv_id: "", title: "", content: "", start_at: 0, end_at: 0, status: 1 };
    const f = {
      title: h("input", { value: v.title, maxlength: 100 }),
      content: h("textarea", { rows: 6, maxlength: 2000, value: v.content }),
      srvID: select([["", "全服"], ...servers.map((s) => [s.srv_id, s.name + "（" + s.srv_id + "）"])], v.srv_id),
      startAt: h("input", { type: "datetime-local", value: toLocalInput(v.start_at) }),
      endAt: h("input", { type: "datetime-local", value: toLocalInput(v.end_at) }),
      status: h("input", { type: "checkbox", checked: v.status === 1 }),
    };
    const err = h("p", { class: "form-error", role: "alert" });
    modal({
      title: n ? "编辑公告" : "新建公告", wide: true,
      body: h("div", { class: "form-grid" }, field("标题", f.title), field("区服", f.srvID), field("内容", f.content), field("开始时间", f.startAt), field("结束时间", f.endAt, "留空 = 不结束"),
        h("label", { class: "field check" }, f.status, " 显示"), err),
      actions: [{ label: "取消", onclick: (c) => c() }, {
        label: "保存", kind: "primary", onclick: async (close) => {
          const r = buildNoticePayload({ title: f.title.value, content: f.content.value, srvID: f.srvID.value, startAt: f.startAt.value, endAt: f.endAt.value, status: f.status.checked });
          if (r.error) { err.textContent = r.error; return; }
          try { const res = n ? await put(`/api/notices/${n.id}`, r.payload) : await post("/api/notices", r.payload); close(); const m = pushSummary(res); toast(m.text, m.kind); await load(); } catch (e) { err.textContent = e.message; }
        },
      }],
    });
  }

  root.append(
    canWrite ? h("div", { class: "toolbar" }, h("button", { class: "btn primary", type: "button", text: "新建公告", onclick: () => edit(null) })) : null,
    h("p", { class: "hint", text: "玩家在游戏邮箱的「公告」标签里看到这些公告：显示中、已到开始时间、未结束，并且是全服或其所在区服的。保存或删除时会实时通知在线玩家；已过期的公告要等玩家重新登录才会消失。" }), box);
  await load();
}
