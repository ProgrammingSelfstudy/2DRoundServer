#!/usr/bin/env python3
"""Extract the horcrux (魂器) tables of the restored client (horcrux_data.lua): the heroes that have one (their attribute
slots), per level the cost, the hero conditions and the attributes of each slot, the level-back rewards, and the activation cost.

usage: extract_horcrux.py LUA_DIR OUT_JSON
"""
import json, sys, os
sys.path.insert(0, os.path.dirname(__file__))
from luadump import dump

P = "Config.HorcruxData."
m = dump(sys.argv[1], "horcrux_data.lua", [P + n for n in ("data_constant", "data_partner_horcrux", "data_horcrux_attr", "data_fallback_reward")])
const = m[P + "data_constant"]
heroes = {k: {"name": v["horcrux_name"], "slots": v["horcrux_id"]} for k, v in m[P + "data_partner_horcrux"].items()}
levels = {}
for k, v in m[P + "data_horcrux_attr"].items():
    levels[k] = {"loss": v["loss"], "conds": v["conds"], "num": v["num"],
                 "attr": {a[4:]: v[a] for a in v if a.startswith("attr") and v[a]}}
fallback = {str(r["lev"]): {"loss": r["loss"], "reward": r["reward"]} for r in m[P + "data_fallback_reward"]}
out = {"source": "restored client horcrux_data.lua", "awaken_loss": const["partner_awaken_loss"]["val"],
       "awaken_limit": const["partner_awaken_limit"]["val"], "heroes": heroes, "levels": levels, "fallback": fallback}
assert len(heroes) == 97 and len(levels) == 36 and len(fallback) == 35
json.dump(out, open(sys.argv[2], "w"), separators=(",", ":"), ensure_ascii=False)
print(len(heroes), len(levels), len(fallback), out["awaken_loss"], out["awaken_limit"])
