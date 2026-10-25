#!/usr/bin/env python3
"""Extract the old planes ("秘境" / 位面, secret_dun_data.lua) rules from the restored client: constants, the five dungeons
(entry conditions and first-clear rewards), the 387 event kinds and the relics (buffs).

usage: extract_planes_dun.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.SecretDunData."
m = dump(lua_dir, "secret_dun_data.lua", [S + n for n in ("data_const", "data_customs", "data_evt_info", "data_buff", "data_dun_info", "data_max_dun_num")])
m = {k[len(S):]: v for k, v in m.items()}
events = {k: {"type": e["type"], "group": e["group_id"], "ext": e["ext"], "reward": e["reward"], "is_walk": e["is_walk"], "name": e["name"]} for k, e in m["data_evt_info"].items()}
customs = [{"id": i + 1, "first_reward": c["first_reward"], "id_limit": c["id_limit"], "lev_limit": c["lev_limit"], "power_limit": c["power_limit"], "add_radio": c["add_radio"]}
           for i, c in enumerate(m["data_customs"])]
out = {"source": "restored client secret_dun_data.lua",
       "const": {k: v["val"] for k, v in m["data_const"].items() if not isinstance(v["val"], str) or len(v["val"]) < 40},
       "customs": customs, "events": events, "names": [d["name"] for d in m["data_dun_info"]], "max_floors": m["data_max_dun_num"],
       "buffs": {k: {"quality": b["quality"], "name": b["name"]} for k, b in m["data_buff"].items()}}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(events), "events;", len(out["buffs"]), "buffs;", len(customs), "dungeons")
