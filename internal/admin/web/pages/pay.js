import { get, post, put } from "/api.js";
import { h, field, select, badge, table, toast, confirmBox } from "/dom.js";
import { fmtTime } from "/lib.js";

const STATUS = { 0: ["等待付款", "warn"], 1: ["已到账", "ok"], 2: ["已取消", ""] };
const MAX_QR = 600 * 1024;

function readImage(file) {
  return new Promise((resolve, reject) => {
    if (!file) return reject(new Error("没有选择图片"));
    if (!/^image\/(png|jpeg|webp)$/.test(file.type)) return reject(new Error("收款码要是 png、jpeg 或 webp 图片"));
    if (file.size > MAX_QR) return reject(new Error("图片不能超过 600 KB"));
    const r = new FileReader();
    r.onload = () => resolve(r.result);
    r.onerror = () => reject(new Error("读取图片失败"));
    r.readAsDataURL(file);
  });
}

export async function render(root, ctx) {
  const canManage = ctx.can("manage");
  const cfgBox = h("div", {});
  const ordersBox = h("div", {});
  let status = "0";

  async function loadConfig() {
    const cfg = await get("/api/pay/config");
    const channel = (key, name) => {
      const img = cfg[key] ? h("img", { src: cfg[key], alt: name, style: "width:160px;height:160px;object-fit:contain;border:1px solid #ccc;border-radius:6px" }) : h("p", { class: "hint", text: "还没有设置" });
      const file = h("input", { type: "file", accept: "image/png,image/jpeg,image/webp" });
      return h("div", { class: "panel", style: "min-width:200px" }, h("h3", { text: name + "收款码" }), img,
        canManage ? h("div", {}, file,
          h("button", { class: "btn primary", type: "button", text: "上传", onclick: async () => {
            try { await put("/api/pay/config", { key, value: await readImage(file.files[0]) }); toast("已保存"); await loadConfig(); } catch (e) { toast(e.message, "err"); }
          } }),
          cfg[key] ? h("button", { class: "btn danger", type: "button", text: "清除", onclick: async () => {
            if (!(await confirmBox(`清除${name}收款码？`, { danger: true, confirm: "清除" }))) return;
            try { await put("/api/pay/config", { key, value: "" }); await loadConfig(); } catch (e) { toast(e.message, "err"); }
          } }) : null) : null);
    };
    const notice = h("textarea", { rows: 3, maxlength: 300, value: cfg.notice || "", disabled: !canManage });
    cfgBox.replaceChildren(
      h("div", { style: "display:flex;gap:16px;flex-wrap:wrap" }, channel("qr_wechat", "微信"), channel("qr_alipay", "支付宝")),
      field("付款页提示（玩家付款时看到，最多 300 字）", notice),
      canManage ? h("button", { class: "btn", type: "button", text: "保存提示", onclick: async () => {
        try { await put("/api/pay/config", { key: "notice", value: notice.value }); toast("已保存"); } catch (e) { toast(e.message, "err"); }
      } }) : null);
  }

  async function act(o, what) {
    const note = what === "confirm" ? "" : "";
    if (!(await confirmBox(what === "confirm" ? `确认订单 ${o.order_no}（${o.role}，${o.product}，¥${o.amount_cents / 100}）已收到款并发货？` : `取消订单 ${o.order_no}？`, { danger: what !== "confirm", confirm: what === "confirm" ? "确认到账" : "取消订单" }))) return;
    try {
      const res = await post(`/api/pay/orders/${o.order_no}/${what}`, { note });
      toast(what === "confirm" ? (res.refreshed ? "已发货，玩家在线已刷新" : "已发货，玩家下次登录可见") : "已取消", res.push_error ? "warn" : "ok");
      await loadOrders();
    } catch (e) { toast(e.message, "err"); }
  }

  async function loadOrders() {
    const r = await get(`/api/pay/orders?limit=100${status === "" ? "" : "&status=" + status}`);
    ordersBox.replaceChildren(table(["订单号", "角色", "商品", "金额", "状态", "时间", "处理人", canManage ? "操作" : ""].filter(Boolean), r.items.map((o) => {
      const [text, kind] = STATUS[o.status] || ["?", ""];
      return h("tr", {},
        h("td", { text: o.order_no }), h("td", { text: `${o.role || ""}（${o.rid}）` }), h("td", { text: o.product || o.charge_id }),
        h("td", { text: "¥" + o.amount_cents / 100 }), h("td", {}, badge(text, kind)), h("td", { text: fmtTime(o.status === 1 ? o.paid_at : o.created_at) }),
        h("td", { text: (o.operator || "") + (o.note ? "：" + o.note : "") }),
        canManage ? h("td", { class: "actions" }, o.status === 0 ? [
          h("button", { class: "btn primary", type: "button", text: "确认到账", onclick: () => act(o, "confirm") }),
          h("button", { class: "btn danger", type: "button", text: "取消", onclick: () => act(o, "cancel") })] : null) : null);
    })));
  }

  async function manual() {
    const { items } = await get("/api/pay/products");
    const rid = h("input", { type: "number", min: 1, placeholder: "角色 ID" });
    const product = select(items.map((p) => [String(p.id), `${p.name}（¥${p.yuan}）`]), String(items[0]?.id ?? ""));
    const confirmNow = h("input", { type: "checkbox", checked: true });
    const note = h("input", { maxlength: 100, placeholder: "备注，例如：微信转账 30 元" });
    return h("div", { class: "panel" }, h("h3", { text: "补单（玩家线下已付款）" }),
      h("div", { class: "form-grid" }, field("角色 ID", rid), field("商品", product), field("备注", note), h("label", { class: "field check" }, confirmNow, " 立即发货")),
      h("button", { class: "btn primary", type: "button", text: "提交", onclick: async () => {
        try {
          const res = await post("/api/pay/orders", { rid: Number(rid.value), charge_id: Number(product.value), confirm: confirmNow.checked, note: note.value });
          toast(confirmNow.checked ? "已发货" : "订单已创建，到下面确认"); rid.value = ""; await loadOrders(); return res;
        } catch (e) { toast(e.message, "err"); }
      } }));
  }

  const filter = select([["0", "等待付款"], ["1", "已到账"], ["2", "已取消"], ["", "全部"]], status);
  filter.addEventListener("change", () => { status = filter.value; loadOrders().catch((e) => toast(e.message, "err")); });
  root.append(
    h("p", { class: "hint", text: "玩家在游戏里点充值后，手机浏览器会打开付款页，显示这里设置的收款码。玩家付款后，在下面找到订单、核对收款，点「确认到账」才会发货。没有收到款的订单不要确认。" }),
    h("h2", { text: "收款码" }), cfgBox,
    h("h2", { text: "订单" }), h("div", { class: "toolbar" }, filter, h("button", { class: "btn", type: "button", text: "刷新", onclick: () => loadOrders().catch((e) => toast(e.message, "err")) })), ordersBox,
    canManage ? await manual() : null);
  await loadConfig();
  await loadOrders();
}
