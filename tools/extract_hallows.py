#!/usr/bin/env python3
"""Extract the hallows ("神技") from the restored client: the five hallows with their 24 steps (cost, attributes, lucky cap),
the skill levels, the seal (刻印) caps, the refine levels (hallows_refine_data.lua), the unlock tasks and the looks ("幻化") with their tasks.

usage: extract_hallows.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
H = "Config.HallowsData."
R = "Config.HallowsRefineData."
h = dump(lua_dir, "hallows_data.lua", [H + n for n in ("data_const", "data_base", "data_info_table", "data_skill_up_table", "data_trace_cost_table", "data_task", "data_task_info", "data_skill_attr", "data_magic", "data_magic_task", "data_max_lev", "data_skill_max_lev")])
r = dump(lua_dir, "hallows_refine_data.lua", [R + n for n in ("data_const", "data_refine", "data_max_lev")])
a = dump(lua_dir, "attr_data.lua", ["Config.AttrData.data_key_to_id"])
h = {k[len(H):]: v for k, v in h.items()}
r = {k[len(R):]: v for k, v in r.items()}

info = {k: {"attr": v[3], "loss": v[4], "max_lucky": v[5]} for k, v in h["data_info_table"].items()}
skill = {k: {"lose": v[0], "skill_bid": v[1], "lev_limit": v[2]} for k, v in h["data_skill_up_table"].items()}
trace = {k: v[0] for k, v in h["data_trace_cost_table"].items()}
const = {k: v["val"] for k, v in h["data_const"].items() if k != "game_rule"}
out = {
    "source": "restored client hallows_data.lua, hallows_refine_data.lua",
    "const": const,
    "refine_open_lev": r["data_const"]["open_lev"]["val"],
    "attr_ids": a["Config.AttrData.data_key_to_id"],
    "base": h["data_base"],
    "max_lev": h["data_max_lev"],
    "skill_max_lev": h["data_skill_max_lev"],
    "info": info,
    "skill_up": skill,
    "trace": trace,
    "skill_attr": h["data_skill_attr"],
    "tasks": h["data_task"],
    "magic": h["data_magic"],
    "magic_tasks": h["data_magic_task"],
    "refine": r["data_refine"],
    "refine_max_lev": r["data_max_lev"],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(info), "steps;", len(skill), "skill levels;", len(out["tasks"]), "tasks;", len(out["magic"]), "looks;", [len(x) for x in out["refine"]], "refine levels")
