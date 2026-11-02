import { can, type Capability } from "../auth/permissions.js";
import { moduleCategories } from "./moduleCategories.js";

// The groups of the side menu (a folder with a page for each part) and what a role may see of them. The pages are the same
// shape as the app's Menu.MenuOptions, kept here without the app's aliases so the tree can be tested.
export interface MenuNode {
  path: string;
  name: string;
  meta: {
    icon: string;
    title: string;
    isHide: boolean;
    isFull: boolean;
    isAffix: boolean;
    isKeepAlive: boolean;
    permission?: Capability;
    section?: string;
    category?: string;
  };
  children?: MenuNode[];
}

interface Leaf {
  path: string;
  name: string;
  title: string;
  permission?: Capability;
  section?: string;
  category?: string;
}
function leafMenu(l: Leaf): MenuNode {
  return {
    path: l.path,
    name: l.name,
    meta: {
      icon: "Menu",
      title: l.title,
      isHide: false,
      isFull: false,
      isAffix: false,
      isKeepAlive: false,
      permission: l.permission ?? "read",
      ...(l.section ? { section: l.section } : {}),
      ...(l.category ? { category: l.category } : {})
    }
  };
}
// A group is only a folder of the side menu: its path is never visited, each page below it has its own address.
function groupMenu(path: string, title: string, icon: string, leaves: Leaf[]): MenuNode {
  return {
    path,
    name: path.slice(1).replace(/\//g, "-"),
    meta: { icon, title, isHide: false, isFull: false, isAffix: false, isKeepAlive: false, permission: "read" },
    children: leaves.map(leafMenu)
  };
}

// 充值管理: one page for each job.
export const payGroup = groupMenu("/menu/pay", "充值管理", "Wallet", [
  { path: "/pay/orders", name: "pay-orders", title: "订单管理" },
  { path: "/pay/products", name: "pay-products", title: "商品列表" },
  { path: "/pay/config", name: "pay-config", title: "收款配置" },
  { path: "/pay/manual", name: "pay-manual", title: "手工补单", permission: "manage" }
]);
// 运营数据: the figures of the days, the retention of new roles and the use of the commands.
export const analyticsGroup = groupMenu("/menu/analytics", "运营数据", "DataLine", [
  { path: "/analytics/overview", name: "analytics-overview", title: "数据概览", section: "overview" },
  { path: "/analytics/retention", name: "analytics-retention", title: "新角色留存", section: "retention" },
  { path: "/analytics/commands", name: "analytics-commands", title: "协议使用", section: "commands" }
]);
// 玩法状态: the server first, then the server-wide modules of each kind.
export const gameStatusGroup = groupMenu("/menu/game-status", "玩法状态", "DataAnalysis", [
  { path: "/game-status/overview", name: "game-status-overview", title: "区服概况", section: "overview" },
  { path: "/game-status/tables", name: "game-status-tables", title: "区服 / 公会数据表", section: "tables" },
  ...moduleCategories
    .filter(c => c.hasGlobal)
    .map(c => ({
      path: `/game-status/c/${c.key}`,
      name: `game-status-${c.key}`,
      title: c.title,
      section: "category",
      category: c.key
    }))
]);
// 模块总表: the registered modules, a list for each kind.
export const modulesGroup = groupMenu(
  "/menu/modules",
  "模块总表",
  "Grid",
  moduleCategories.map(c => ({ path: `/modules/${c.key}`, name: `modules-${c.key}`, title: c.title, category: c.key }))
);

/** The node as the role may see it: pages it can use, and a group only while it has a page left. */
export function visibleMenu(menu: MenuNode, role: unknown): MenuNode | undefined {
  if (!can(role, menu.meta.permission)) return undefined;
  if (!menu.children) return menu;
  const children = menu.children.map(c => visibleMenu(c, role)).filter((c): c is MenuNode => !!c);
  return children.length ? { ...menu, children } : undefined;
}
/** Every page that has a route of its own (the pages inside the groups). */
export const pageMenus: MenuNode[] = [payGroup, analyticsGroup, gameStatusGroup, modulesGroup].flatMap(g => g.children ?? []);
