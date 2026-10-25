#!/usr/bin/env python3
"""Extract the planes ("位面" / 长环岛冒险) rules from the restored client: constants, maps, events, the merchant's goods,
the reward tables, the relics (buffs) and the war order (planes_data.lua, planes_war_order_data.lua).

usage: extract_planes.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
P = "Config.PlanesData."
W = "Config.PlanesWarOrderData."
p = dump(lua_dir, "planes_data.lua", [P + n for n in ("data_const", "data_customs", "data_evt_info", "data_shop_info", "data_reward_info", "data_buff")])
w = dump(lua_dir, "planes_war_order_data.lua", [W + n for n in ("data_constant", "data_lev_reward_list", "data_advance_card_list", "data_period_day")])
p = {k[len(P):]: v for k, v in p.items()}
w = {k[len(W):]: v for k, v in w.items()}
events = {}
for k, e in p["data_evt_info"].items():
    events[k] = {"type": e["type"], "group": e["group_id"], "ext": e["ext"], "reward_list": e["reward_list"], "reward": e["reward"], "exchange": e["exchange_id"], "name": e["name"]}
out = {
    "source": "restored client planes_data.lua, planes_war_order_data.lua",
    "const": {k: v["val"] for k, v in p["data_const"].items() if not isinstance(v["val"], str) or len(v["val"]) < 40},
    "customs": p["data_customs"], "events": events, "shop": p["data_shop_info"], "rewards": p["data_reward_info"],
    "buffs": {k: {"quality": b["quality"], "name": b["name"], "range": b["range"]} for k, b in p["data_buff"].items()},
    "order": {"constant": {k: v["val"] for k, v in w["data_constant"].items() if not isinstance(v["val"], str) or len(v["val"]) < 40},
              "levels": w["data_lev_reward_list"], "cards": w["data_advance_card_list"], "days": w["data_period_day"]},
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(events), "events;", len(out["buffs"]), "buffs;", len(out["shop"]), "goods;", len(out["rewards"]), "reward tables")
