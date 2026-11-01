import type { Router } from "vue-router";
import { session } from "./runtime";
import { useAuthStore } from "@/stores/modules/auth";
import { useTabsStore } from "@/stores/modules/tabs";
import { useKeepAliveStore } from "@/stores/modules/keepAlive";

// Called after Pinia is installed and before the router starts navigation.
export function installAuthLifecycle(router: Router) {
  const auth = useAuthStore();
  let token = session.state.token;
  let role = session.state.admin?.role;
  let expiry: ReturnType<typeof setTimeout> | undefined;
  function scheduleExpiry() {
    clearTimeout(expiry);
    if (session.state.token)
      expiry = setTimeout(
        () => {
          session.expire();
          scheduleExpiry();
        },
        Math.min(Math.max(session.state.expiresAt * 1000 - Date.now(), 1), 2147483647)
      );
  }
  const unsubscribe = session.subscribe(state => {
    scheduleExpiry();
    if (role !== state.admin?.role) {
      role = state.admin?.role;
      useTabsStore().setTabs([]);
      useKeepAliveStore().setKeepAliveName([]);
      const route = router.currentRoute.value;
      if (state.admin && route.matched.length && !route.meta.public && !auth.hasPermission(route.meta.permission)) {
        void router.replace("/403");
      }
    }
    if (token !== state.token) {
      token = state.token;
      useTabsStore().setTabs([]);
      useKeepAliveStore().setKeepAliveName([]);
    }
    if (!state.token && router.currentRoute.value.meta.public !== true && router.currentRoute.value.matched.length) {
      if (!auth.loginNotice) auth.loginNotice = "登录已失效，请重新登录";
      void router.replace({ path: "/login", query: { redirect: router.currentRoute.value.fullPath } });
    }
  });
  scheduleExpiry();
  async function refresh() {
    if (!session.state.token || document.visibilityState === "hidden") return;
    try {
      await auth.verifySession();
    } catch {
      /* store clears session; subscriber redirects */
    }
  }
  window.addEventListener("focus", refresh);
  document.addEventListener("visibilitychange", refresh);
  // While a page stays open, refresh disabled/changed accounts within a minute.
  const interval = setInterval(refresh, 60000);
  if (import.meta.hot)
    import.meta.hot.dispose(() => {
      unsubscribe();
      clearTimeout(expiry);
      clearInterval(interval);
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", refresh);
    });
}
