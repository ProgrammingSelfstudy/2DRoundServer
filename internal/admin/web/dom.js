// A tiny DOM builder. Text always goes in through textContent / createTextNode, and
// innerHTML is never used anywhere: whatever a player or an operator typed is shown
// as text, never parsed as markup. No inline style either (the CSP forbids it).

export function h(tag, props, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (v == null || v === false) continue;
    if (k === "class") el.className = v;
    else if (k === "text") el.textContent = v;
    else if (k.startsWith("on") && typeof v === "function") el.addEventListener(k.slice(2), v);
    else if (k === "value" || k === "checked" || k === "disabled" || k === "selected") el[k] = v;
    else if (/^(innerHTML|outerHTML|srcdoc)$/i.test(k) || /^on/i.test(k) || k === "style") throw new Error(`h(): refusing attribute ${k}`);
    else el.setAttribute(k, v === true ? "" : String(v));
  }
  append(el, kids);
  return el;
}

function append(el, kids) {
  for (const k of kids.flat(Infinity)) {
    if (k == null || k === false) continue;
    el.append(k instanceof Node ? k : document.createTextNode(String(k)));
  }
}

export const clear = (el) => el.replaceChildren();

// ---- toast ----
let toastBox;
export function toast(message, kind = "ok") {
  if (!toastBox) {
    toastBox = h("div", { class: "toasts", role: "status", "aria-live": "polite" });
    document.body.append(toastBox);
  }
  const t = h("div", { class: `toast ${kind}`, text: message });
  toastBox.append(t);
  setTimeout(() => t.remove(), kind === "err" ? 7000 : 3500);
}

// ---- modal ----
// modal({title, body, actions:[{label, kind, onclick}]}) -> {close}. Escape and the backdrop close it.
export function modal({ title, body, actions = [], wide = false, onClose }) {
  const prevFocus = document.activeElement;
  let closed = false;
  const close = () => {
    if (closed) return;
    closed = true;
    backdrop.remove();
    document.removeEventListener("keydown", onKey);
    if (prevFocus && prevFocus.focus) prevFocus.focus();
    if (onClose) onClose();
  };
  const onKey = (e) => e.key === "Escape" && close();
  const box = h("div", { class: "modal" + (wide ? " wide" : ""), role: "dialog", "aria-modal": "true", "aria-label": title },
    h("h3", { text: title }), h("div", { class: "modal-body" }, body),
    h("div", { class: "modal-actions" }, actions.map((a) => h("button", { class: `btn ${a.kind || ""}`, type: "button", text: a.label, onclick: () => a.onclick(close) }))));
  const backdrop = h("div", { class: "backdrop", onmousedown: (e) => e.target === backdrop && close() }, box);
  document.body.append(backdrop);
  document.addEventListener("keydown", onKey);
  const first = box.querySelector("input,select,textarea,button");
  if (first) first.focus();
  return { close };
}

// confirmBox resolves true only when the operator presses the confirm button;
// Cancel, Escape and a click outside all resolve false.
export function confirmBox(message, { title = "请确认", confirm = "确定", danger = false } = {}) {
  return new Promise((resolve) => {
    modal({
      title, body: h("p", { text: message }), onClose: () => resolve(false),
      actions: [
        { label: "取消", onclick: (close) => close() },
        { label: confirm, kind: danger ? "danger" : "primary", onclick: (close) => { resolve(true); close(); } },
      ],
    });
  });
}

// ---- form helpers ----
export function field(label, input, hint) {
  return h("label", { class: "field" }, h("span", { class: "label", text: label }), input, hint ? h("small", { class: "hint", text: hint }) : null);
}

export function select(options, value, props = {}) {
  return h("select", props, options.map(([v, label]) => h("option", { value: String(v), selected: String(v) === String(value), text: label })));
}

export function badge(text, kind = "") { return h("span", { class: `badge ${kind}`, text }); }

export function table(columns, rows) {
  return h("div", { class: "table-wrap" }, h("table", {},
    h("thead", {}, h("tr", {}, columns.map((c) => h("th", { text: c })))),
    h("tbody", {}, rows.length ? rows : h("tr", {}, h("td", { class: "empty", colspan: columns.length, text: "没有数据" })))));
}

export function pager(info, onGo) {
  return h("div", { class: "pager" },
    h("span", { text: info.total === 0 ? "共 0 条" : `${info.from}–${info.to} / 共 ${info.total} 条` }),
    h("button", { class: "btn", type: "button", text: "上一页", disabled: !info.hasPrev, onclick: () => onGo(info.prevOffset) }),
    h("button", { class: "btn", type: "button", text: "下一页", disabled: !info.hasNext, onclick: () => onGo(info.nextOffset) }));
}
