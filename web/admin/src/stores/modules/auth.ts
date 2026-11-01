import { defineStore } from "pinia";
import { computed, ref, shallowRef } from "vue";
import { api, session } from "@/auth/runtime";
import { managementPages, actionAllowed } from "@/domain/management";
import { canOperate } from "@/domain/operations";
import { can } from "@/auth/permissions";
import { StaleSessionError } from "@/api/client";
import { menusForRole } from "@/routers/menu";
import { getAllBreadcrumbList, getFlatMenuList, getShowMenuList } from "@/utils";

export const useAuthStore = defineStore("2dround-auth", () => {
  const state = shallowRef(session.state);
  const unsubscribe = session.subscribe(value => {
    state.value = value;
  });
  if (import.meta.hot) import.meta.hot.dispose(unsubscribe);
  const admin = computed(() => state.value.admin);
  const revision = computed(() => state.value.revision);
  const authenticated = computed(() => !!admin.value && !!state.value.token);
  const routeName = ref("");
  const loginNotice = ref("");
  const authMenuList = computed(() => menusForRole(admin.value?.role));
  // Only migrated actions are exposed. Write/manage capabilities are available
  // via hasPermission; they do not grant controls for unmigrated functionality.
  const authButtonListGet = computed<Record<string, string[]>>(
    (): Record<string, string[]> =>
      admin.value
        ? {
            ...Object.fromEntries(
              managementPages
                .filter(p => can(admin.value?.role, p[2]))
                .map(p => [p[0], ["read", ...(actionAllowed(admin.value?.role, p[0]) ? ["write"] : [])]])
            ),
            dashboard: ["refresh", ...(can(admin.value.role, "write") ? ["broadcast"] : [])],
            environment: ["check"],
            mail: ["read", ...(canOperate(admin.value.role, "mail") ? ["send"] : [])],
            pay: ["read", ...(canOperate(admin.value.role, "pay") ? ["config", "create", "confirm", "cancel"] : [])],
            activities: ["read", ...(canOperate(admin.value.role, "activities") ? ["create", "edit", "copy", "delete"] : [])],
            servers: ["read", ...(can(admin.value.role, "manage") ? ["create", "edit", "toggle", "delete"] : [])],
            roles: [
              "read",
              ...(can(admin.value.role, "write")
                ? ["ban", "unban", "kick", "level", "vip", "grant", "heroes", "skins", "looks"]
                : [])
            ]
          }
        : {}
  );
  const authMenuListGet = computed(() => authMenuList.value);
  const showMenuListGet = computed(() => getShowMenuList(authMenuList.value));
  const flatMenuListGet = computed(() => getFlatMenuList(authMenuList.value));
  const breadcrumbListGet = computed(() => getAllBreadcrumbList(authMenuList.value));
  const hasPermission = (need: unknown) => can(admin.value?.role, need);
  const setRouteName = (name: string) => {
    routeName.value = name;
  };

  let pending: { revision: number; promise: Promise<void> } | undefined;
  async function verifySession() {
    session.expire();
    if (!session.state.token) return false;
    const current = session.state.revision;
    if (!pending || pending.revision !== current) {
      const promise = (async () => {
        try {
          const identity = await api<unknown>("/me");
          if (!session.confirm(identity, current)) throw new StaleSessionError();
        } catch (error) {
          if (
            !(error instanceof StaleSessionError) &&
            (current === session.state.revision || (!session.state.token && session.state.revision === current + 1))
          ) {
            loginNotice.value = error instanceof Error ? error.message : "无法验证登录状态，请重新登录";
            if (current === session.state.revision) session.clear(current);
          }
          throw error;
        }
      })();
      pending = { revision: current, promise };
      void promise
        .finally(() => {
          if (pending?.promise === promise) pending = undefined;
        })
        .catch(() => {});
    }
    await pending.promise;
    return authenticated.value;
  }
  async function login(username: string, password: string) {
    loginNotice.value = "";
    session.clear();
    const current = session.state.revision;
    const result = await api<{ token: string; expires_at: number }>("/login", {
      method: "POST",
      body: { username, password },
      public: true
    });
    if (current !== session.state.revision) throw new StaleSessionError();
    session.start(result.token, result.expires_at);
    // Use fresh /me rather than a browser-cached role or the login response's admin.
    await verifySession();
    loginNotice.value = "";
  }
  function logout() {
    loginNotice.value = "已退出登录";
    session.clear();
  }
  return {
    admin,
    revision,
    authenticated,
    loginNotice,
    authMenuList,
    routeName,
    authButtonListGet,
    authMenuListGet,
    showMenuListGet,
    flatMenuListGet,
    breadcrumbListGet,
    hasPermission,
    setRouteName,
    verifySession,
    login,
    logout
  };
});
