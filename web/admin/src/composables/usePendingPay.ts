import { onBeforeUnmount, ref, watch } from "vue";
import { ElNotification } from "element-plus";
import { api } from "@/auth/runtime";
import { useAuthStore } from "@/stores/modules/auth";
import router from "@/routers";
import type { Order } from "@/domain/operations";

const INTERVAL = 15000;
const LIMIT = 200;
const baseTitle = document.title;
// Shared by the menu badge and the pay page, so one poller serves both.
export const pendingPay = ref(0);
let known = new Set<string>();
let first = true;
let timer: ReturnType<typeof setInterval> | undefined;
let polling = false;

function beep() {
  try {
    const ctx = new AudioContext();
    const osc = ctx.createOscillator();
    osc.connect(ctx.destination);
    osc.frequency.value = 880;
    osc.start();
    osc.stop(ctx.currentTime + 0.25);
    osc.onended = () => void ctx.close();
  } catch {
    /* autoplay blocked or no audio: the notification still shows */
  }
}

async function poll() {
  if (polling) return;
  polling = true;
  try {
    const data = await api<{ items: Order[] }>(`/pay/orders?status=0&limit=${LIMIT}`);
    const fresh = data.items.filter(o => !known.has(o.order_no));
    known = new Set(data.items.map(o => o.order_no));
    pendingPay.value = data.items.length;
    document.title = pendingPay.value ? `(${pendingPay.value}) ${baseTitle}` : baseTitle;
    // The first poll after login only seeds the set: old orders are not "new".
    if (!first && fresh.length) {
      const total = fresh.reduce((sum, o) => sum + o.amount_cents, 0) / 100;
      beep();
      ElNotification({
        title: "有新的待确认充值订单",
        message: `${fresh.length} 笔，合计 ¥${total}，请核对收款后确认到账`,
        type: "warning",
        duration: 0,
        onClick: () => void router.push("/pay")
      });
    }
    first = false;
  } catch {
    /* transient failure or expired session: the next tick retries */
  } finally {
    polling = false;
  }
}

function stop() {
  clearInterval(timer);
  timer = undefined;
  known = new Set();
  first = true;
  pendingPay.value = 0;
  document.title = baseTitle;
}

// Called once from the layout: polls while a signed-in role may read orders.
export function usePendingPay() {
  const auth = useAuthStore();
  watch(
    () => auth.authenticated && auth.hasPermission("read"),
    on => {
      if (!on) return stop();
      void poll();
      timer ??= setInterval(poll, INTERVAL);
    },
    { immediate: true }
  );
  onBeforeUnmount(stop);
}

export const refreshPendingPay = poll;
