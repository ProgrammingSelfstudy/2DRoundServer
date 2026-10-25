#!/usr/bin/env python3
"""Extract the sky island adventure ("空岛冒险") from the restored client: events, constants, floors, skills, the quiz,
the npc dialogs, round rewards, buffs (adventure_data.lua) and the crystal mine (adventure_mine_data.lua), plus the
monsters of every unit group an event fights (unit_data*.lua + the hero template's active skills).

usage: extract_adventure.py LUA_DIR PARTNERS_JSON SKILLS_JSON OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import glob, json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, partners_json, skills_json, out_json = sys.argv[1:5]
A = "Config.AdventureData."
M = "Config.AdventureMineData."
a = dump(lua_dir, "adventure_data.lua", [A + n for n in (
    "data_adventure_event_table", "data_map", "data_adventure_const", "data_floor_reward", "data_battle_res", "data_skill_data",
    "data_adventure_answer", "data_adventure_kind_answer", "data_adventure_npc_data", "data_round_reward_list", "data_buff")])
m = dump(lua_dir, "adventure_mine_data.lua", [M + n for n in (
    "data_const", "data_floor_data", "data_box_reward", "data_attenuation_reward", "data_mine_unlock_info")])
a = {k[len(A):]: v for k, v in a.items()}
m = {k[len(M):]: v for k, v in m.items()}

# events: [evt_id, evt_type, lose, effect, unit_id, base_items, desc, effect_str, box_show_item, power, lock, res_id, shadow, face, handle_type, auto_open]
names = ["evt_id", "evt_type", "lose", "effect", "unit_id", "base_items", "desc", "effect_str", "box_show_item", "power", "lock", "res_id", "shadow", "face", "handle_type", "auto_open"]
events = {int(k): dict(zip(names, v)) for k, v in a["data_adventure_event_table"].items()}

groups, monsters = {}, {}
for f in sorted(glob.glob(lua_dir + "/unit_data[0-9]*.lua")):
    src = open(f, encoding="utf-8").read()
    n = f.rsplit("unit_data", 1)[1][:-4]
    r = dump(lua_dir, f, ["Config.UnitData%s.data_unit1_table" % n, "Config.UnitData%s.data_unit2_table" % n])
    groups.update({int(k): v for k, v in (r["Config.UnitData%s.data_unit1_table" % n] or {}).items()})
    monsters.update({int(k): v for k, v in (r["Config.UnitData%s.data_unit2_table" % n] or {}).items()})
partners = json.load(open(partners_json, encoding="utf-8"))["partners"]
skills = json.load(open(skills_json, encoding="utf-8"))["skills"]


def monster_skills(head):
    p = partners.get(str(head))
    if not p or not p["stars"]:
        return []
    star = sorted(p["stars"], key=int)[0]
    return [sk[1] for sk in p["stars"][star].get("skills", []) if skills.get(str(sk[1]), {}).get("type") == "active_skill"]


# mines: [id, name, star, pos, base_items, hook_items, robot_power, units, robot_power_repair, robot_lev, robot_name, max_time, primary_loss, senior_loss, res_id]
mine_names = ["id", "name", "star", "pos", "base_items", "hook_items", "robot_power", "units", "robot_power_repair", "robot_lev", "robot_name",
              "max_time", "primary_loss", "senior_loss", "res_id"]
mines = {int(k): dict(zip(mine_names, v)) for k, v in dump(lua_dir, "adventure_mine_data.lua", [M + "data_mine_data_table"])[M + "data_mine_data_table"].items()}

units, missing = {}, 0
wanted = [e["unit_id"] for e in events.values()] + [u[0] for mi in mines.values() for u in mi["units"]]
for uid in wanted:
    if not uid or uid in units:
        continue
    g = groups.get(uid)
    if not g:
        missing += 1
        continue
    mons = []
    for mid in g[11:16]:
        mm = monsters.get(mid) if mid else None
        if mm:
            mons.append({"bid": mid, "name": mm[1], "career": mm[2], "camp": mm[3], "lev": mm[5], "head": mm[12], "skills": monster_skills(mm[12])})
    units[uid] = {"group": g[1], "monsters": mons}

out = {"source": "restored client adventure_data.lua, adventure_mine_data.lua, unit_data*.lua",
       "events": {str(k): v for k, v in sorted(events.items())}, "units": {str(k): v for k, v in sorted(units.items())},
       "map": a["data_map"], "const": a["data_adventure_const"], "floors": a["data_floor_reward"], "battle_res": a["data_battle_res"],
       "skills": a["data_skill_data"], "answers": a["data_adventure_answer"], "answer_options": a["data_adventure_kind_answer"],
       "npc": a["data_adventure_npc_data"], "round_rewards": a["data_round_reward_list"], "buffs": a["data_buff"], "mine": m, "mines": {str(k): v for k, v in sorted(mines.items())}}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(mines), "mines;", len(events), "events;", len(units), "unit groups;", missing, "without a group;", len(a["data_adventure_answer"]), "questions;", len(a["data_adventure_npc_data"]), "npc events")
