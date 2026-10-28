import { get } from "/api.js";
import { h, table } from "/dom.js";
import { fmtTime, truncate } from "/lib.js";

export async function render(root) {
  const r = await get("/api/reports?limit=200");
  root.append(h("p", { class: "hint", text: "玩家在聊天里举报别人的记录，最近 200 条。聊天记录客户端会附上，但服务端不保存。可以到「角色」页按编号查人，再封禁或踢下线。" }),
    table(["ID", "举报人", "被举报人", "区服", "类型", "内容", "时间"], (r.items || []).map((x) => h("tr", {},
      h("td", { text: x.id }), h("td", { text: x.rid }), h("td", { text: x.target_rid }), h("td", { text: x.target_srv_id }), h("td", { text: x.type }),
      h("td", { text: truncate(x.msg, 60), title: x.msg }), h("td", { text: fmtTime(x.created_at) })))));
}
