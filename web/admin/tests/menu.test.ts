import test from "node:test";
import assert from "node:assert/strict";
import { categoryOf, categoryTitle, moduleCategories } from "../src/domain/moduleCategories.js";
import { productKind, isSafeImage, yuanText } from "../src/domain/payPages.js";
import { analyticsGroup, gameStatusGroup, modulesGroup, pageMenus, payGroup, visibleMenu, type MenuNode } from "../src/domain/menuTree.js";

// What the side menu shows of the four groups to a role.
const menusForRole = (role: unknown) =>
  [payGroup, analyticsGroup, gameStatusGroup, modulesGroup].map(m => visibleMenu(m, role)).filter((m): m is MenuNode => !!m);

// The keys of the runtime registry (internal/game AdminModules(), 2026-11-02): "role" and "global" modules together.
const registry = [
  "activity",
  "activitybox",
  "adventure",
  "adventure_mine",
  "adventure_mine_world",
  "animate",
  "arenachamp",
  "arenamany",
  "arenateam",
  "arenateamrole",
  "artifact",
  "chargeacc",
  "crossarena",
  "crosschamp",
  "cyclegift",
  "dailygift",
  "daycharge",
  "daysrank",
  "dial",
  "dramapass",
  "element",
  "elfin",
  "elitematch",
  "elitepass",
  "emoguoshi",
  "empower",
  "encounter",
  "endless_rank",
  "endless_trail",
  "escort",
  "escort_board",
  "firstcharge_*",
  "fortunebag",
  "growway",
  "guildmarket",
  "guildmarket:*",
  "guildredbag",
  "guildwar",
  "halidom",
  "hallows",
  "heaven",
  "heroexpedit",
  "holelite",
  "holyeqm",
  "homepet",
  "homeworld",
  "homeworld_board",
  "horcrux",
  "ladder",
  "levgift",
  "limitexercise",
  "limitgift",
  "loginday",
  "luckydog",
  "mergegoal",
  "monopoly",
  "monopoly_g*",
  "monthcard",
  "monthfund",
  "newwarorder",
  "onecentgift",
  "peakformations",
  "petard",
  "planes",
  "planesafk",
  "practisetower",
  "practisetower_rank",
  "presage",
  "primus",
  "recruithero",
  "redbagday",
  "returnaction",
  "sandybeach",
  "sevengoal",
  "skinlottery",
  "smashegg",
  "smashegg_log",
  "snatch",
  "stoneshop",
  "sweet",
  "termbegins",
  "timecollect",
  "timesummon",
  "titlelev",
  "touchgold",
  "trainingcamp",
  "treasure",
  "vedio",
  "voyage",
  "warorder",
  "whiteday",
  "worldlev",
  "yearmonster"
];
// The ones of them that are server-wide.
const serverWide = [
  "adventure_mine_world",
  "arenachamp",
  "arenamany",
  "arenateam",
  "crossarena",
  "crosschamp",
  "daysrank",
  "dial",
  "dramapass",
  "elitematch",
  "endless_rank",
  "escort_board",
  "guildmarket:*",
  "guildredbag",
  "guildwar",
  "homeworld_board",
  "ladder",
  "levgift",
  "monopoly_g*",
  "petard",
  "practisetower_rank",
  "primus",
  "returnaction",
  "sandybeach",
  "smashegg_log",
  "snatch",
  "sweet",
  "termbegins",
  "treasure",
  "worldlev",
  "yearmonster"
];
const flat = (menus: MenuNode[]): MenuNode[] => menus.flatMap(m => [m, ...flat(m.children ?? [])]);

test("every registered module belongs to a category, and only worldlev is in the catch-all", () => {
  for (const key of registry) {
    const c = categoryOf(key);
    assert.ok(moduleCategories.some(x => x.key === c), key);
    if (c === "other") assert.equal(key, "worldlev");
  }
  assert.equal(categoryOf("worldlev"), "other");
  assert.equal(categoryOf("a_module_nobody_listed"), "other");
});
test("a module is in one category; families match by prefix", () => {
  const seen = new Map<string, string>();
  for (const c of moduleCategories)
    for (const k of c.keys) {
      assert.equal(seen.get(k), undefined, `${k} is in ${seen.get(k)} and ${c.key}`);
      seen.set(k, c.key);
    }
  assert.equal(categoryOf("firstcharge_21001"), "pay");
  assert.equal(categoryOf("guildmarket:77"), "guild");
  assert.equal(categoryOf("guildmarket"), "guild");
  assert.equal(categoryOf("monopoly_g5"), "guild");
  assert.equal(categoryOf("monopoly"), "event");
  assert.equal(categoryTitle("pay"), "充值与礼包");
});
test("a category with a server-wide module has a game status page, and only those", () => {
  for (const c of moduleCategories) {
    const has = serverWide.some(k => categoryOf(k) === c.key);
    assert.equal(c.hasGlobal, has, c.key);
  }
});

test("each group of the side menu lists pages that all have an address, no address twice", () => {
  const menus = menusForRole("superadmin");
  const groups = menus.filter(m => m.children);
  assert.deepEqual(groups.map(g => g.meta.title), ["充值管理", "运营数据", "玩法状态", "模块总表"]);
  const leaves = flat(menus).filter(m => !m.children);
  const paths = leaves.map(l => l.path);
  assert.equal(new Set(paths).size, paths.length);
  assert.equal(new Set(leaves.map(l => l.name)).size, leaves.length);
  for (const g of groups) assert.ok(!paths.includes(g.path), `${g.path} is a folder, not a page`);
  for (const p of pageMenus) assert.ok(paths.includes(p.path), p.path);
  // a page of a group is a page of its own: the old single pages are not in the menu
  for (const gone of ["/pay", "/analytics", "/game-status", "/modules"]) assert.ok(!paths.includes(gone), gone);
});
test("pay pages: one for each job; the manual order page is for superadmin only", () => {
  const pay = (role: string) => menusForRole(role).find(m => m.path === "/menu/pay")!.children!.map(c => c.path);
  assert.deepEqual(pay("superadmin"), ["/pay/orders", "/pay/products", "/pay/config", "/pay/manual"]);
  assert.deepEqual(pay("operator"), ["/pay/orders", "/pay/products", "/pay/config"]);
  assert.deepEqual(pay("viewer"), ["/pay/orders", "/pay/products", "/pay/config"]);
});
test("the analytics, status and module groups have a page for each part and kind", () => {
  const sub = (path: string) => menusForRole("viewer").find(m => m.path === path)!.children!;
  assert.deepEqual(sub("/menu/analytics").map(c => c.meta.section), ["overview", "retention", "commands"]);
  const status = sub("/menu/game-status");
  assert.deepEqual(status.slice(0, 2).map(c => c.meta.section), ["overview", "tables"]);
  assert.ok(status.slice(2).every(c => c.meta.section === "category" && c.meta.category));
  assert.ok(!status.some(c => c.meta.category === "grow"), "nothing server-wide to show for the growth modules");
  assert.deepEqual(sub("/menu/modules").map(c => c.meta.category), moduleCategories.map(c => c.key));
  assert.equal(menusForRole(undefined).length, 0);
  assert.equal(menusForRole("nobody").length, 0);
});
test("product kinds, safe images and money", () => {
  assert.equal(productKind(3), "钻石充值档位");
  assert.equal(productKind(30001), "快乐币面板");
  assert.equal(productKind(30020), "快乐币面板");
  assert.equal(productKind(30021), "礼包与活动商品");
  assert.equal(productKind(301), "礼包与活动商品");
  assert.equal(isSafeImage("data:image/png;base64,AAAA"), true);
  assert.equal(isSafeImage("data:image/svg+xml;base64,AAAA"), false);
  assert.equal(isSafeImage("javascript:alert(1)"), false);
  assert.equal(yuanText(3800), "¥38.00");
});
