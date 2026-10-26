#!/usr/bin/env python3
"""Extract the year monster action (actionyearmonster, holiday_nian_data.lua): the constants, the 34 event kinds of the map, the
damage bands, the rank rewards, the exchange rows, the red bag thresholds, the three monsters (the timer boss 97001, the
gold boss 97003 and the small one 97004, as unit_data*.lua + the hero template's active skills) and the walkable grid of map
100001 (config_map_block.lua, config_map.lua).

usage: extract_yearmonster.py LUA_DIR PARTNERS_JSON SKILLS_JSON OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
Needs a LuaJIT on the PATH.
"""
import glob, json, re, subprocess, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, partners_json, skills_json, out_json = sys.argv[1:5]
K = "Config.HolidayNianData."
raw = dump(lua_dir, "holiday_nian_data.lua", [K + n for n in ("data_const", "data_evt_info", "data_harm_reward", "data_rank_reward", "data_exchange", "data_redbag_progress", "data_dialogue", "data_board")])
raw = {k[len(K):]: v for k, v in raw.items()}


def lua_row(text):
    out, in_str, esc, quote = [], False, False, ""
    for ch in text:
        if in_str:
            if esc:
                esc = False
                out.append(ch)
            elif ch == "\\":
                esc = True
                out.append(ch)
            elif ch == quote:
                in_str = False
                out.append('"')
            else:
                out.append(ch)
        elif ch in ('"', "'"):
            in_str, quote = True, ch
            out.append('"')
        elif ch == "{":
            out.append("[")
        elif ch == "}":
            out.append("]")
        else:
            out.append(ch)
    text = re.sub(r",\s*\]", "]", "".join(out))
    text = re.sub(r"\[\s*,", "[null,", text)
    text = re.sub(r",\s*,", ",null,", text)
    return json.loads(text)


def table_rows(path, table):
    src = open(path, encoding="utf-8").read()
    i = src.find(table + " = {")
    if i < 0:
        return {}
    blk = src[i:]
    blk = blk[:blk.find("\n}\n")]
    return {int(m.group(1)): lua_row(m.group(2).replace('\\"', '"').replace("\\\\", "\\"))
            for m in re.finditer(r'\n\t\[(\d+)(?:\.0)?\] = "((?:[^"\\]|\\.)*)",?', blk)}


groups, monsters = {}, {}
for f in sorted(glob.glob(lua_dir + "/unit_data[0-9]*.lua")):
    src = open(f, encoding="utf-8").read()
    for name, dest in (("data_unit1_table", groups), ("data_unit2_table", monsters)):
        m = re.search(r"(Config\.UnitData\d*\." + name + r")", src)
        if m:
            dest.update(table_rows(f, m.group(1)))
partners = json.load(open(partners_json, encoding="utf-8"))["partners"]
skills = json.load(open(skills_json, encoding="utf-8"))["skills"]


def monster_skills(head):
    p = partners.get(str(head))
    if not p or not p["stars"]:
        return []
    star = sorted(p["stars"], key=int)[0]
    return [sk[1] for sk in p["stars"][star].get("skills", []) if skills.get(str(sk[1]), {}).get("type") == "active_skill"]



def unit(uid):
    g = groups.get(uid)
    mid = next((x for x in (g or [])[11:16] if x), None)
    m = monsters.get(mid) if mid else None
    if not m:
        raise SystemExit("no monster for unit %d" % uid)
    return {"bid": mid, "name": m[1], "career": m[2], "camp": m[3], "lev": m[5], "head": m[12], "skills": monster_skills(m[12])}


LUA = r'''
Config = {}
dofile(arg[1] .. "/config_map_block.lua")
dofile(arg[1] .. "/config_map.lua")
local id = 100001
local rows = Config.MapBlock.data(id)
local m = Config.Map[id]
local parts = {}
for y, row in ipairs(rows) do parts[#parts + 1] = "[" .. table.concat(row, ",") .. "]" end
io.write(string.format('{"cols":%d,"rows":%d,"width":%d,"height":%d,"tile_w":%d,"tile_h":%d,"block":[%s]}', m.cols, m.rows, m.width, m.height, m.tile_w, m.tile_h, table.concat(parts, ",")))
'''
grid = json.loads(subprocess.run(["luajit", "-", lua_dir], input=LUA, capture_output=True, text=True, check=True).stdout)

keep = ("exchange_desc", "holiday_nian_desc", "holiday_nian_fight_desc", "holiday_nian_tribute_desc", "holiday_nian_tribute_desc")
const = {k: v["val"] for k, v in raw["data_const"].items() if k not in keep}
evts = {k: {"type": v["type"], "name": v["name"], "is_walk": v["is_walk"], "reward": v["reward"], "ext": v["ext"], "skills": v["boss_skill_id"]} for k, v in raw["data_evt_info"].items()}
out = {
    "source": "restored client holiday_nian_data.lua, unit_data*.lua, config_map*.lua",
    "const": const,
    "events": evts,
    "harm": {k: [[b["dps_low"], b["dps_high"]] for b in v] for k, v in raw["data_harm_reward"].items()},
    "ranks": [{"min": r["rank_high"], "max": r["rank_low"], "reward": r["reward"]} for r in raw["data_rank_reward"]],
    "exchange": [{"id": r["id"], "loss": r["loss"], "gain": r["gain"], "time": r["time"]} for r in raw["data_exchange"]],
    "redbag": raw["data_redbag_progress"],
    "units": {"timer": unit(97001), "gold": unit(97003), "small": unit(97004)},
    "map": grid,
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print("yearmonster:", len(evts), "events;", len(out["exchange"]), "exchange rows;", len(out["ranks"]), "rank rows; map", grid["cols"], "x", grid["rows"])
