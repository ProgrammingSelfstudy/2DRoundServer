#!/usr/bin/env python3
"""Extract the Wazuni fire festival ("和之国火祭", the monopoly board) from the restored client (monopoly_maps_data.lua,
monopoly_dungeons_data.lua): constants, the four maps with their explore goal, the guard bosses of each map and their
rewards, the story talks and their rewards, and the per-lap rank awards.

usage: extract_monopoly.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
M, D = "Config.MonopolyMapsData.", "Config.MonopolyDungeonsData."
mm = dump(lua_dir, "monopoly_maps_data.lua", [M + n for n in ("data_const", "data_customs", "data_drama", "data_dialog", "data_map_info")])
md = dump(lua_dir, "monopoly_dungeons_data.lua", [D + n for n in ("data_const", "data_boss_info", "data_award")])
const = {k: v["val"] for k, v in mm[M + "data_const"].items() if not k.startswith("monopoly_rule")}
const.update({k: v["val"] for k, v in md[D + "data_const"].items()})
out = {
    "source": "restored client monopoly_maps_data.lua, monopoly_dungeons_data.lua",
    "const": const,
    "maps": [{"id": c["id"], "name": c["name"], "max_develop": c["max_develop"], "need": c["develop"]} for c in mm[M + "data_customs"]],
    "layouts": [{"id": r["id"], "max_grid": r["max_grid"]} for r in mm[M + "data_map_info"]],
    "bosses": [[{"boss_id": b["boss_id"], "name": b["boss_name"], "develop": b["develop"], "base_id": b["base_id"], "drop": b["drop_id"], "reward": b["reward"]} for b in grp] for grp in md[D + "data_boss_info"]],
    "dramas": [{"type": d["type"], "reward": d["reward"]} for d in mm[M + "data_drama"]],
    "talks": [{"id": t["id"], "step": t["step_id"], "answers": len(t["answer"])} for t in mm[M + "data_dialog"][0].values()],
    "flag_answers": len(mm[M + "data_dialog"][1][next(iter(mm[M + "data_dialog"][1]))]["answer"]),
    "awards": md[D + "data_award"],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out["maps"]), "maps;", [len(b) for b in out["bosses"]], "bosses;", len(out["talks"]), "talks")
