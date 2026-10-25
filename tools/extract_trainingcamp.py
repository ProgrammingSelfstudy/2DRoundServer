#!/usr/bin/env python3
"""Extract the training camp (training_camp_data.lua): the constants and the 8 lessons, each with the heroes lent to
the player (unit groups 96011.., their monster is the hero), the heroes that must be in the team and the enemy group
(target_id 96010.., its monsters are the enemies), the monsters taken from unit_data*.lua.

usage: extract_trainingcamp.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import glob, json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
T = "Config.TrainingCampData."
tc = dump(lua_dir, "training_camp_data.lua", [T + "data_const", T + "data_info"])
infos = tc[T + "data_info"]
if isinstance(infos, dict):
    infos = [infos[i] for i in sorted(infos, key=int)]

groups, monsters = {}, {}
for n in (1, 2, 3):
    f = "unit_data%d.lua" % n
    u = "Config.UnitData%d." % n
    m = dump(lua_dir, f, [u + "data_unit1_table", u + "data_unit2_table"])
    groups.update({int(k): v for k, v in m[u + "data_unit1_table"].items()})
    monsters.update({int(k): v for k, v in m[u + "data_unit2_table"].items()})


def monster(mid):
    r = monsters[mid]  # bid, name, career, camp, sex, lev, ..., head_icon [12], ..., star [15]
    return {"bid": r[0], "name": r[1], "career": r[2], "camp": r[3], "lev": r[5], "head": r[12], "star": r[15]}


def group_monsters(gid):
    return [monster(mid) for mid in groups[gid][11:16] if mid]


stages = []
for r in infos:
    stages.append({
        "id": r["id"], "name": r["name"], "type": r["type"], "formation": r["formation"], "unlock": r["unlock"],
        "ban_pos": r["ban_pos"], "reward": r["reward"], "enemy": group_monsters(r["target_id"]),
        "enemy_name": groups[r["target_id"]][1],
        "partners": [{"unit": u, "hero": group_monsters(u)[0]} for u in r["partner_id"]],
        "required": [{"unit": u, "pos": p, "hero": group_monsters(u)[0]} for u, p in r["required_partner"]],
    })
const = {k: v["val"] for k, v in tc[T + "data_const"].items() if isinstance(v["val"], (int, float))}
json.dump({"source": "restored client training_camp_data.lua + unit_data*.lua", "const": const, "stages": stages},
          open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(stages), "stages;", sum(len(s["partners"]) for s in stages), "lent heroes")
