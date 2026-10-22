import { get, post, put, del, ApiError } from "/api.js";
import { h, modal, field, select, badge, table, toast, confirmBox } from "/dom.js";
import { buildServerPayload, fmtTime, toLocalInput } from "/lib.js";

export async function render(root, ctx) {
  const list = h("div", {});
  const manage = ctx.can("manage");

  async function reload() {
    const { items } = await get("/api/servers");
    list.replaceChildren(table(
      ["区服 ID", "名称", "地址", "状态", "标记", "分组 / 分区", "开服时间", "合服到", manage ? "操作" : ""].filter(Boolean),
      items.map((s) => h("tr", {},
        h("td", { text: s.srv_id }), h("td", { text: s.name }), h("td", { text: `${s.host}:${s.port}` }),
        h("td", {}, s.status === 1 ? badge("开放", "ok") : badge("维护中", "warn")),
        h("td", {}, s.is_new ? badge("新服") : null, " ", s.is_recommend ? badge("推荐") : null),
        h("td", { text: `${s.group_id} / ${s.zone_id}` }), h("td", { text: fmtTime(s.open_time) }), h("td", { text: s.main_srv_id || "-" }),
        manage ? h("td", { class: "actions" },
          h("button", { class: "btn", type: "button", text: "编辑", onclick: () => edit(s) }),
          h("button", { class: "btn", type: "button", text: s.status === 1 ? "设为维护" : "恢复开放", onclick: () => toggle(s) }),
          h("button", { class: "btn danger", type: "button", text: "删除", onclick: () => remove(s) })) : null))));
  }

  const toBody = (s, status) => ({
    srv_id: s.srv_id, name: s.name, platform: s.platform, zone_id: s.zone_id, host: s.host, port: s.port, ws_url: s.ws_url,
    status, is_new: s.is_new, is_recommend: s.is_recommend, open_time: s.open_time, main_srv_id: s.main_srv_id, group_id: s.group_id,
  });

  async function toggle(s) {
    const to = s.status === 1 ? 0 : 1;
    if (!(await confirmBox(to === 0 ? `把 ${s.srv_id} 设为维护？玩家将无法登录该服（已在线的不受影响）。` : `恢复 ${s.srv_id} 开放？`, { danger: to === 0 }))) return;
    try { await put(`/api/servers/${s.srv_id}`, toBody(s, to)); toast("已更新"); await reload(); } catch (e) { toast(e.message, "err"); }
  }

  async function remove(s) {
    if (!(await confirmBox(`删除区服 ${s.srv_id}？只有没有角色的区服才能删除。`, { danger: true, confirm: "删除" }))) return;
    try { await del(`/api/servers/${s.srv_id}`); toast("已删除"); await reload(); } catch (e) { toast(e.message, "err"); }
  }

  function edit(s) {
    const isNew = !s;
    const v = s || { srv_id: "", name: "", platform: "symlf", zone_id: 1, host: "127.0.0.1", port: 8800, ws_url: "", status: 1, is_new: false, is_recommend: false, open_time: 0, main_srv_id: "", group_id: 1 };
    const f = {
      srvID: h("input", { value: v.srv_id, disabled: !isNew, placeholder: "symlf_2" }),
      name: h("input", { value: v.name }), platform: h("input", { value: v.platform }),
      zoneID: h("input", { type: "number", value: v.zone_id, min: 1 }), groupID: h("input", { type: "number", value: v.group_id, min: 1 }),
      host: h("input", { value: v.host }), port: h("input", { type: "number", value: v.port, min: 1, max: 65535 }),
      wsURL: h("input", { value: v.ws_url, placeholder: "小游戏用的 wss 地址（可空）" }),
      status: select([[1, "开放"], [0, "维护中"]], v.status),
      isNew: h("input", { type: "checkbox", checked: v.is_new }), isRecommend: h("input", { type: "checkbox", checked: v.is_recommend }),
      openTime: h("input", { type: "datetime-local", value: toLocalInput(v.open_time) }),
      mainSrvID: h("input", { value: v.main_srv_id, placeholder: "合服后的主服（可空）" }),
    };
    const err = h("p", { class: "form-error", role: "alert" });
    modal({
      title: isNew ? "新建区服" : `编辑 ${v.srv_id}`, wide: true,
      body: h("div", { class: "form-grid" }, field("区服 ID", f.srvID), field("名称", f.name), field("平台", f.platform), field("分组", f.groupID), field("分区号", f.zoneID),
        field("主机", f.host), field("端口", f.port), field("WebSocket 地址", f.wsURL), field("状态", f.status), field("开服时间", f.openTime), field("合服到", f.mainSrvID),
        h("label", { class: "field check" }, f.isNew, " 新服标记"), h("label", { class: "field check" }, f.isRecommend, " 推荐标记"), err),
      actions: [{ label: "取消", onclick: (c) => c() }, {
        label: "保存", kind: "primary", onclick: async (close) => {
          const r = buildServerPayload({ srvID: f.srvID.value, name: f.name.value, platform: f.platform.value, zoneID: f.zoneID.value, groupID: f.groupID.value,
            host: f.host.value, port: f.port.value, wsURL: f.wsURL.value, status: f.status.value, isNew: f.isNew.checked, isRecommend: f.isRecommend.checked,
            openTime: f.openTime.value, mainSrvID: f.mainSrvID.value });
          if (r.error) { err.textContent = r.error; return; }
          try {
            if (isNew) await post("/api/servers", r.payload); else await put(`/api/servers/${v.srv_id}`, r.payload);
            close(); toast("已保存"); await reload();
          } catch (e) { err.textContent = e.message; }
        },
      }],
    });
  }

  root.append(manage ? h("div", { class: "toolbar" }, h("button", { class: "btn primary", type: "button", text: "新建区服", onclick: () => edit(null) })) : null, list);
  await reload();
}
