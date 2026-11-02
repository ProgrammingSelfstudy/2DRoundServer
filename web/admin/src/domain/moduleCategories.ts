// The game modules of the admin registry (internal/game/adminreg.go) by what they are about. The module list, the module pages of
// the game status and their menus follow this, so nothing is shown as one list of a hundred. A key not listed here is shown under
// "其他", so a module added to the registry is never hidden; add it to its category when it is.
export interface ModuleCategory {
  key: string;
  title: string;
  /** some module of the category is a server-wide one (the game status has a page for it) */
  hasGlobal: boolean;
  keys: readonly string[];
}
export const moduleCategories: readonly ModuleCategory[] = [
  {
    key: "pay",
    title: "充值与礼包",
    hasGlobal: true,
    keys: [
      "chargeacc",
      "cyclegift",
      "dailygift",
      "daycharge",
      "firstcharge_*",
      "levgift",
      "limitgift",
      "monthcard",
      "monthfund",
      "onecentgift",
      "privilege",
      "stoneshop",
      "warorder",
      "newwarorder",
      "elitepass"
    ]
  },
  {
    key: "arena",
    title: "竞技与排名",
    hasGlobal: true,
    keys: [
      "arenachamp",
      "arenamany",
      "arenateam",
      "arenateamrole",
      "crossarena",
      "crosschamp",
      "daysrank",
      "elitematch",
      "endless_rank",
      "ladder",
      "peakformations",
      "primus",
      "vedio"
    ]
  },
  {
    key: "guild",
    title: "公会",
    hasGlobal: true,
    keys: ["guildmarket", "guildmarket:*", "guildredbag", "guildwar", "redbagday", "monopoly_g*"]
  },
  {
    key: "event",
    title: "限时活动",
    hasGlobal: true,
    keys: [
      "activity",
      "activitybox",
      "animate",
      "dial",
      "fortunebag",
      "holelite",
      "loginday",
      "luckydog",
      "mergegoal",
      "monopoly",
      "petard",
      "presage",
      "recruithero",
      "returnaction",
      "sandybeach",
      "sevengoal",
      "skinlottery",
      "smashegg",
      "smashegg_log",
      "snatch",
      "sweet",
      "termbegins",
      "timecollect",
      "timesummon",
      "touchgold",
      "treasure",
      "whiteday",
      "yearmonster"
    ]
  },
  {
    key: "grow",
    title: "养成",
    hasGlobal: false,
    keys: ["artifact", "elfin", "emoguoshi", "empower", "encounter", "growway", "hallows", "halidom", "holyeqm", "horcrux", "titlelev", "trainingcamp"]
  },
  {
    key: "explore",
    title: "副本与探索",
    hasGlobal: true,
    keys: [
      "adventure",
      "adventure_mine",
      "adventure_mine_world",
      "dramapass",
      "element",
      "endless_trail",
      "escort",
      "escort_board",
      "heaven",
      "heroexpedit",
      "limitexercise",
      "planes",
      "planesafk",
      "practisetower",
      "practisetower_rank",
      "voyage"
    ]
  },
  { key: "home", title: "家园", hasGlobal: true, keys: ["homepet", "homeworld", "homeworld_board"] },
  { key: "other", title: "其他", hasGlobal: true, keys: ["worldlev"] }
];

const exact = new Map<string, string>();
const prefixes: [string, string][] = [];
for (const c of moduleCategories)
  for (const k of c.keys) {
    // "firstcharge_*" and "guildmarket:*" name a family: the registry has one entry for it, the saved states have one key each
    if (k.endsWith("*")) prefixes.push([k.slice(0, -1), c.key]);
    else exact.set(k, c.key);
  }

/** The category key of a module key (a registry key or the key of one saved state); "other" for one nobody listed. */
export function categoryOf(key: string): string {
  const hit = exact.get(key);
  if (hit) return hit;
  for (const [prefix, category] of prefixes) if (key.startsWith(prefix)) return category;
  return "other";
}
export function categoryTitle(key: string): string {
  return moduleCategories.find(c => c.key === key)?.title ?? key;
}
