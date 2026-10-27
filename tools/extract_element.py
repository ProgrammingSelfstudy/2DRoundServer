#!/usr/bin/env python3
"""Extract the element temple (元素圣殿, Config.ElementTempleData) from the restored client: the five bases (weekdays they open, the
four boss groups of each), the 20 groups of 12 customs (camp limit, first and sweep rewards, monsters of the unit group), the
challenge constants, the buy costs and the rank rewards.

usage: extract_element.py LUA_DIR UNIT_DIR OUT_JSON
  LUA_DIR   .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
  UNIT_DIR  .../outputs/lua-restored-paths/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump
from unitdata import load_units, monsters_of

lua_dir, unit_dir, out_json = sys.argv[1:4]
K = "Config.ElementTempleData."
m = dump(lua_dir, "element_temple_data.lua", [K + n for n in ("data_const", "data_base", "data_customs", "data_buy_count", "data_award", "data_monster")])
groups, monsters = load_units(unit_dir)
customs = []
for g in m[K + "data_customs"]:
    for c in g:
        customs.append({"group": c["group"], "id": c["id"], "limit": c["limit"], "limit_dun_id": c["limit_dun_id"], "power": c["power"],
                        "first": c["first_reward"], "auto": c["auto_reward"], "unit_id": c["unit_id"],
                        "monsters": monsters_of(groups, monsters, c["unit_id"])})
out = {"source": "restored client element_temple_data.lua + unit_data*.lua",
       "const": {k: v["val"] for k, v in m[K + "data_const"].items()},
       "bases": [{"type": b["type"], "name": b["name"], "open_day": b["open_day"], "groups": b["group"]} for b in m[K + "data_base"]],
       "customs": customs,
       "buy": [{"cost": b["cost"], "num": b["num"]} for b in m[K + "data_buy_count"]],
       "award": [{"min": a["min"], "max": a["max"], "items": a["items"]} for a in m[K + "data_award"]],
       "boss": {str(b["group"]): {"name": b["name"], "head": b["head_id"]} for b in m[K + "data_monster"]}}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(customs), "customs;", sum(1 for c in customs if not c["monsters"]), "without monsters")
