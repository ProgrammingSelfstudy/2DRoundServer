#!/usr/bin/env python3
"""Extract the elite match pass ("段位赛令牌活动", arena_elite_war_order_data.lua): per period the levels (wins needed, free and
paid rewards).

usage: extract_elitepass.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.ArenaEliteWarOrderData."
m = dump(lua_dir, "arena_elite_war_order_data.lua", [S + "data_lev_reward_list"])[S + "data_lev_reward_list"]
items = m.items() if isinstance(m, dict) else enumerate(m, 1)
periods = {str(p): [{"lev": l["lev"], "wins": l["win_count"], "reward": l["reward"], "rmb_reward": l["rmb_reward"]}
                    for l in sorted(levs.values() if isinstance(levs, dict) else levs, key=lambda l: l["lev"])] for p, levs in items}
json.dump({"source": "restored client arena_elite_war_order_data.lua", "periods": periods}, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("elitepass:", {p: len(v) for p, v in periods.items()})
