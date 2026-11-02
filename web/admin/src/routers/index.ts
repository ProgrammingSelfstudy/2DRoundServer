import { createRouter, createWebHashHistory } from "vue-router";
import NProgress from "@/config/nprogress";
import { useAuthStore } from "@/stores/modules/auth";
import {
  dashboardMenu,
  serversMenu,
  rolesMenu,
  mailMenu,
  activitiesMenu,
  managementMenus,
  environmentMenu,
  pageMenus
} from "./menu";

// The page behind each address of the groups in the side menu. Several addresses can share one view: it reads its part
// (meta.section) and its kind of modules (meta.category) from the route.
function viewOf(name: string) {
  if (name.startsWith("pay-")) return () => import(`../views/pay/${name.slice(4)}.vue`);
  if (name.startsWith("analytics-")) return () => import("@/views/analytics/index.vue");
  if (name.startsWith("game-status-")) return () => import("@/views/game-status/index.vue");
  return () => import("@/views/modules/index.vue");
}

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: "/", redirect: "/dashboard" },
    // the addresses from before the pages were split
    { path: "/pay", redirect: "/pay/orders" },
    { path: "/analytics", redirect: "/analytics/overview" },
    { path: "/modules", redirect: "/modules/pay" },
    {
      path: "/game-status",
      // the server and guild pages link here with ?srv= and, for a guild, &gid=
      redirect: to => ({ path: to.query.gid ? "/game-status/tables" : "/game-status/overview", query: to.query })
    },
    { path: "/login", name: "login", component: () => import("@/views/login/index.vue"), meta: { public: true, title: "登录" } },
    {
      path: "/layout",
      component: () => import("@/layouts/index.vue"),
      children: [
        {
          path: dashboardMenu.path,
          name: dashboardMenu.name,
          meta: { ...dashboardMenu.meta },
          component: () => import("@/views/dashboard/index.vue")
        },
        {
          path: serversMenu.path,
          name: serversMenu.name,
          meta: { ...serversMenu.meta },
          component: () => import("@/views/servers/index.vue")
        },
        {
          path: rolesMenu.path,
          name: rolesMenu.name,
          meta: { ...rolesMenu.meta },
          component: () => import("@/views/roles/index.vue")
        },
        {
          path: mailMenu.path,
          name: mailMenu.name,
          meta: { ...mailMenu.meta },
          component: () => import("@/views/mail/index.vue")
        },
        {
          path: activitiesMenu.path,
          name: activitiesMenu.name,
          meta: { ...activitiesMenu.meta },
          component: () => import("@/views/activities/index.vue")
        },
        ...pageMenus.map(menu => ({
          path: menu.path,
          name: menu.name,
          meta: { ...menu.meta },
          component: viewOf(menu.name)
        })),
        ...managementMenus.map(menu => ({
          path: menu.path,
          name: menu.name,
          meta: { ...menu.meta },
          component: () => import(`../views/${menu.name}/index.vue`)
        })),
        {
          path: environmentMenu.path,
          name: environmentMenu.name,
          meta: { ...environmentMenu.meta },
          component: () => import("@/views/environment/index.vue")
        }
      ]
    },
    {
      path: "/403",
      component: () => import("@/components/ErrorMessage/403.vue"),
      meta: { title: "没有权限", permission: "read" }
    },
    {
      path: "/:pathMatch(.*)*",
      component: () => import("@/components/ErrorMessage/404.vue"),
      meta: { title: "页面不存在", permission: "read" }
    }
  ]
});
router.beforeEach(async to => {
  NProgress.start();
  const auth = useAuthStore();
  // Revalidate on each navigation: role changes/disabled accounts take effect.
  try {
    const signedIn = await auth.verifySession();
    if (to.meta.public) return signedIn ? "/dashboard" : true;
    if (!signedIn) return { path: "/login", query: { redirect: to.fullPath } };
    if (!auth.hasPermission(to.meta.permission)) return "/403";
    auth.setRouteName(String(to.name || ""));
  } catch {
    if (!to.meta.public) return { path: "/login", query: { redirect: to.fullPath } };
  }
  return true;
});
router.afterEach(to => {
  document.title = `${to.meta.title || "管理后台"} - ${import.meta.env.VITE_GLOB_APP_TITLE}`;
  NProgress.done();
});
router.onError(() => NProgress.done());
export default router;
