#!/usr/bin/env python3
"""Extract the rank rewards of the cross champion match (arena_cluster_champion_data.lua): min rank, max rank, items.

usage: extract_crosschampion.py LUA_DIR OUT_JSON
"""
import json, sys, os
sys.path.insert(0, os.path.dirname(__file__))
from luadump import dump

S = "Config.ArenaClusterChampionData."
m = dump(sys.argv[1], "arena_cluster_champion_data.lua", [S + "data_awards"])[S + "data_awards"]
rows = m if isinstance(m, list) else [m[k] for k in sorted(m, key=int)]
out = {"source": "restored client arena_cluster_champion_data.lua", "awards": [{"min": r["min"], "max": r["max"], "items": [i for i in r["items"] if i[1] > 0]} for r in rows]}
assert len(out["awards"]) == 12
json.dump(out, open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out["awards"]))
