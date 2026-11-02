import { managementPages } from "@/domain/management";
import { analyticsGroup, gameStatusGroup, modulesGroup, pageMenus, payGroup, visibleMenu } from "@/domain/menuTree";
import type { Role } from "@/auth/permissions";

export const dashboardMenu: Menu.MenuOptions = {
  path: "/dashboard",
  name: "dashboard",
  meta: {
    icon: "DataAnalysis",
    title: "概览",
    isHide: false,
    isFull: false,
    isAffix: true,
    isKeepAlive: false,
    permission: "read"
  }
};
export const environmentMenu: Menu.MenuOptions = {
  path: "/environment",
  name: "environment",
  meta: {
    icon: "Monitor",
    title: "开发环境",
    isHide: false,
    isFull: false,
    isAffix: false,
    isKeepAlive: false,
    permission: "read"
  }
};
export const serversMenu: Menu.MenuOptions = {
  path: "/servers",
  name: "servers",
  meta: {
    icon: "Monitor",
    title: "区服管理",
    isHide: false,
    isFull: false,
    isAffix: false,
    isKeepAlive: false,
    permission: "read"
  }
};
export const rolesMenu: Menu.MenuOptions = {
  path: "/roles",
  name: "roles",
  meta: { icon: "User", title: "角色管理", isHide: false, isFull: false, isAffix: false, isKeepAlive: false, permission: "read" }
};
function sectionMenu(path: string, title: string, icon: string): Menu.MenuOptions {
  return {
    path: `/${path}`,
    name: path,
    meta: { icon, title, isHide: false, isFull: false, isAffix: false, isKeepAlive: false, permission: "read" }
  };
}
export const mailMenu = sectionMenu("mail", "邮件管理", "Message");
export const activitiesMenu = sectionMenu("activities", "活动管理", "Calendar");

export const managementMenus = managementPages.map(([path, title, permission]) => ({
  ...sectionMenu(path, title, "Menu"),
  meta: { ...sectionMenu(path, title, "Menu").meta, permission }
}));
export { payGroup, analyticsGroup, gameStatusGroup, modulesGroup, pageMenus };
export function menusForRole(role: Role | undefined) {
  return [
    dashboardMenu,
    serversMenu,
    rolesMenu,
    mailMenu,
    payGroup,
    activitiesMenu,
    ...managementMenus,
    analyticsGroup,
    gameStatusGroup,
    modulesGroup,
    environmentMenu
  ]
    .map(menu => visibleMenu(menu, role))
    .filter((menu): menu is Menu.MenuOptions => !!menu);
}
