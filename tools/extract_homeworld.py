#!/usr/bin/env python3
"""Extract the home world (家园): home_data.lua, the constants, the furniture, the suits, the figures and the home coin table.

usage: extract_homeworld.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.HomeData."
names = ("data_const", "data_suit", "data_figure", "data_suit_award", "data_home_coin", "data_suit_soft", "data_home_storey")
m = dump(lua_dir, "home_data.lua", [S + n for n in names])
m = {k[len(S):]: v for k, v in m.items()}


def rows(t):
    if isinstance(t, list):
        return t
    return [t[k] for k in sorted(t, key=int)]


# data_home_unit is a table of strings, each one a Lua list: bid, name, soft, set_id, tiles 1, tiles 2, offset 1, offset 2,
# type, dir, desc, res, icon, grid_desc, effect_count, source
src = open(lua_dir + "/home_data.lua", encoding="utf-8").read()
body = src[src.index("data_home_unit_table = {"):src.index("Config.HomeData.data_home_storey_length")]
units = []
for line in re.findall(r'^\t\[(\d+)\] = "(.*)",\s*$', body, re.M):
    raw = line[1].replace('\\"', '"')
    v = json.loads(raw.replace("{", "[").replace("}", "]"))
    units.append({"bid": v[0], "name": v[1], "soft": v[2], "set": v[3], "area": len(v[4]), "type": v[8], "effect": v[14]})
assert len(units) == 80, len(units)

c = m["data_const"]
out = {
    "source": "restored client home_data.lua",
    "const": {k: v["val"] for k, v in c.items() if not k.endswith("rule") and not k.endswith("tips")},
    "units": units,
    "suits": [{"set": r["set_id"], "name": r["name"], "num": r["num"], "comfort": r["comfort"]} for r in rows(m["data_suit"])],
    "suit_soft": [[{"num": e["num"], "soft": e["all_soft"]} for e in sorted(t.values(), key=lambda e: e["num"])] for t in rows(m["data_suit_soft"])],
    "suit_award": [[{"id": e["id"], "num": e["num"], "reward": e["reward"]} for e in sorted(t.values(), key=lambda e: e["id"])] for t in rows(m["data_suit_award"])],
    "figures": [{"id": r["id"], "first": r["is_first"], "loss": r["loss"], "open_soft": r["open_soft"], "tips": r["tips"]} for r in rows(m["data_figure"])],
    "coin": sorted(({"soft": r["soft"], "coin": r["home_coin"]} for r in m["data_home_coin"].values()), key=lambda r: r["soft"]),
    "storey": [{"id": r["id"], "limit": r["limit"]} for r in rows(m["data_home_storey"])],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print({k: len(v) for k, v in out.items() if k != "source"})
