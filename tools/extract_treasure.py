#!/usr/bin/env python3
"""Extract the lucky treasure hunt (dial_data.lua: 幸运探宝 / 高级探宝): the constants, the draw options of the two wheels, the
lucky value awards, the items a wheel position can hold and the pools of the multi draws.

usage: extract_treasure.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
K = "Config.DialData."
d = dump(lua_dir, "dial_data.lua", [K + n for n in ("data_const", "data_get_limit_open", "data_get_lucky_award", "data_get_rand_list", "data_magnificat_list")])
d = {k[len(K):]: v for k, v in d.items()}
c = d["data_const"]


def rows(x):
    return list(x.values()) if isinstance(x, dict) else x


items = []
for lst in rows(d["data_get_rand_list"]):
    for r in rows(lst):
        items.append({"id": r["id"], "type": r["type"], "pos": r["pos"], "item": r["item_id"], "num": r["item_num"]})
items.sort(key=lambda r: r["id"])
opts = [{"type": r["type"], "type2": r["type2"], "count": r["count"], "expend": r["expend_item"], "add_item": r["add_item"], "add_lucky": r["add_lucky"],
         "lev": next((v for k, v in r["limit_open"] if k == "lev"), 0), "vip": next((v for k, v in r["limit_open"] if k == "vip_lev"), 0)}
        for r in rows(d["data_get_limit_open"])]
pools = {}
for i, rounds in enumerate(rows(d["data_magnificat_list"]), 1):
    pools[str(i)] = [{"min": r["min"], "max": r["max"], "award": r["award"]} for r in sorted(rows(rounds), key=lambda r: r["min"])]
out = {"source": "restored client dial_data.lua",
       "free_time": c["free_time"]["val"], "refresh": c["refreash"]["val"][0], "slots": [c["num_1"]["val"], c["num_2"]["val"]],
       "options": sorted(opts, key=lambda r: (r["type"], r["type2"])),
       "lucky": sorted(({"id": r["id"], "type": r["type"], "val": r["lucky_val"], "award": r["award"]} for r in rows(d["data_get_lucky_award"])), key=lambda r: r["id"]),
       "items": items, "pools": pools}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print("treasure:", len(items), "wheel items;", len(opts), "options;", len(pools), "pools")
