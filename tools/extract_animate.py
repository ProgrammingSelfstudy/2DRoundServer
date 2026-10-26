#!/usr/bin/env python3
"""Extract the animate action (animate_action): the lantern festival lottery (holiday_lanterm_festival_data.lua: the wine
item, the cost of each light, the four pools) and the kitchen (holiday_make_data.lua: per camp the recipes, their reward, the
level table and the collect fight).

usage: extract_animate.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
L = "Config.HolidayLantermFestivalData."
lm = dump(lua_dir, "holiday_lanterm_festival_data.lua", [L + n for n in ("data_constant", "data_consume_num", "data_consume_list", "data_rand_list")])
lm = {k[len(L):]: v for k, v in lm.items()}


def rows(x):
    return list(x.values()) if isinstance(x, dict) else x


lantern = {
    "item": lm["data_constant"]["awards_expend_items"]["val"],
    "pool4_cost": lm["data_constant"]["awards_4_expend"]["val"][1],
    "need": [r["need_num"] for r in sorted(rows(lm["data_consume_num"]), key=lambda r: r["id"])],
    "cost": [[r["expend"] for r in sorted(rows(p), key=lambda r: r["count"])] for p in rows(lm["data_consume_list"])],
    "pools": [[{"item": r["item_id"], "num": r["item_num"], "weight": r["pro"], "after": r["must_no_count"]} for r in sorted(rows(p), key=lambda r: r["award_id"])] for p in rows(lm["data_rand_list"])],
}
K = "Config.HolidayMakeData."
km = dump(lua_dir, "holiday_make_data.lua", [K + n for n in ("data_make_list", "data_make_reward_list", "data_make_lev_list", "data_collect_list", "data_master_diff_list")])
km = {k[len(K):]: v for k, v in km.items()}
camps = {}
for camp, recipes in km["data_make_list"].items():
    ids = set(recipes)
    group = next(g for g, rs in km["data_make_reward_list"].items() if set(rs) == ids)
    col = km["data_collect_list"][camp]
    camps[camp] = {
        "group": int(group),
        "recipes": [{"id": r["id"], "name": r["name"], "lev": r["lev"], "exp": r["exp"], "limit": r["limit"], "loss": r["loss"],
                     "reward": km["data_make_reward_list"][group][str(r["id"])]["reward"]} for r in sorted(recipes.values(), key=lambda r: r["id"])],
        "levels": [{"lev": r["lev"], "exp": r["exp"], "reward": r["reward"]} for r in sorted(rows(km["data_make_lev_list"][camp]), key=lambda r: r["lev"])],
        "collect": {"award": col["award"], "combat": col["combat_num"], "buy_limit": col["buy_limit"], "buy_loss": col["buy_loss"]},
    }
json.dump({"source": "restored client holiday_lanterm_festival_data.lua and holiday_make_data.lua", "lantern": lantern, "camps": camps},
          open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("animate:", len(camps), "camps; lantern pools", [len(p) for p in lantern["pools"]], lantern["need"])
