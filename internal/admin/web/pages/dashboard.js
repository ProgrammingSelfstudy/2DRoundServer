import { get, post } from "/api.js";
import { h, toast } from "/dom.js";
import { GATEWAY_DOWN } from "/lib.js";

function card(label, value, note, detail) {
  return h("div", { class: "card" }, h("div", { class: "num", text: value ?? "-" }), h("div", { class: "lbl", text: label }),
    note ? h("small", { class: "hint", text: note, title: detail || "" }) : null);
}

export async function render(root, ctx) {
  const [stats, online] = await Promise.all([get("/api/stats"), get("/api/online")]);
  const down = online.online == null;
  root.append(
    h("div", { class: "cards" },
      card("在线角色", down ? "?" : online.online, down ? GATEWAY_DOWN : null, down ? online.error : ""),
      card("账号", stats.accounts), card("角色", stats.roles), card("区服", stats.servers),
      card("已发邮件", stats.mails), card("公告", stats.notices)),
    h("p", { class: "hint", text: "在线数来自网关的本机控制接口；网关未运行时显示 ?，其余数据来自数据库。" }),
    ctx && ctx.can("write") ? broadcastBox() : null);
}

// broadcastBox sends a message that scrolls across the top of every online player's screen.
function broadcastBox() {
  const msg = h("input", { maxlength: 100, placeholder: "向所有在线玩家滚动播放一条消息（最多 100 字）", class: "wide" });
  const send = h("button", { class: "btn primary", type: "button", text: "广播" });
  send.addEventListener("click", async () => {
    const text = msg.value.trim();
    if (!text) { toast("请输入要广播的内容", "err"); return; }
    send.disabled = true;
    try {
      const res = await post("/api/broadcast", { msg: text });
      toast(`已发送给 ${res.pushed} 个在线角色`);
      msg.value = "";
    } catch (e) {
      toast(e.status === 502 ? GATEWAY_DOWN : e.message, "err");
    } finally { send.disabled = false; }
  });
  msg.addEventListener("keydown", (e) => e.key === "Enter" && send.click());
  return h("div", { class: "broadcast" }, h("h3", { text: "跑马灯广播" }), h("div", { class: "toolbar" }, msg, send),
    h("p", { class: "hint", text: "显示在游戏界面顶部，不进入聊天记录。离线的玩家看不到。" }));
}
