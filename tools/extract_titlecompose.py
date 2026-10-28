#!/usr/bin/env python3
"""Extract the title compositions (Config.HonorData.data_artifact_compound / data_artifact_data of honor_data.lua): per title id and
current level, per number of materials the material item, the coin cost, the success rate (per mille) and the refund on failure;
and the top level of each title.

usage: extract_titlecompose.py LUA_DIR OUT_JSON
"""
import json, sys, os
sys.path.insert(0, os.path.dirname(__file__))
from luadump import dump

P = "Config.HonorData."
m = dump(sys.argv[1], "honor_data.lua", [P + "data_artifact_compound", P + "data_artifact_data"])
comp = {}
for tid, levs in m[P + "data_artifact_compound"].items():
    comp[tid] = {}
    for lev, rows in levs.items():
        rows = list(rows.values()) if isinstance(rows, dict) else rows
        comp[tid][lev] = {str(r["cost_num"]): {"material": r["materials"][0], "coin": r["other_expend"], "rate": r["rate"], "award": r["award"]}
                          for r in rows}
top = {tid: v["max_lev"] for tid, v in m[P + "data_artifact_data"].items()}
assert len(comp) == 44 and len(top) == 44
json.dump({"source": "restored client honor_data.lua", "compound": comp, "max_lev": top}, open(sys.argv[2], "w"), separators=(",", ":"))
print(len(comp), sum(len(v) for v in comp.values()))
