#!/usr/bin/env python3
"""Extract the cross-server arena (跨服竞技场, arena_cluster_data.lua): the constants, the daily boxes by number of
challenges and the season rewards by rank.

usage: extract_crossarena.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.ArenaClusterData."
m = dump(lua_dir, "arena_cluster_data.lua", [S + n for n in ("data_const", "data_daily_award", "data_rank_award")])
m = {k[len(S):]: v for k, v in m.items()}
const = {k: v["val"] for k, v in m["data_const"].items()}
daily = sorted(({"num": int(k), "items": v["items"]} for k, v in m["data_daily_award"].items()), key=lambda r: r["num"])
ranks = sorted(({"min": v["min"], "max": v["max"], "items": v["items"]} for v in (m["data_rank_award"].values() if isinstance(m["data_rank_award"], dict) else m["data_rank_award"])), key=lambda r: r["min"])
assert len(daily) == 3 and len(ranks) == 11 and "init_score" in const
json.dump({"source": "restored client arena_cluster_data.lua", "const": const, "daily": daily, "ranks": ranks}, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("crossarena:", len(const), len(daily), len(ranks))
