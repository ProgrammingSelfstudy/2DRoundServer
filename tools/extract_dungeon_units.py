#!/usr/bin/env python3
"""Extract the monsters of every main-line dungeon from the restored client: per dungeon
(Config.DungeonData.data_drama_dungeon_info) its power and unit group (unit_id), per
group (Config.UnitData*.data_unit1_table) its name and the 1-5 monster ids, per
monster (data_unit2_table) its name, career, camp, level and head template; the
monster's skills are those of its head template in partners.json (the client keeps them
in Config.PartnerData.data_partner_star under "<head>_<star>"), without the passive ones.

The monsters' hp / attack / speed are not in these tables (the client gets them from the
server), so none are exported here.

usage: extract_dungeon_units.py LUA_DIR PARTNERS_JSON SKILLS_JSON OUT_JSON
  LUA_DIR  .../outputs/lua-restored-paths/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import glob, json, re, sys

lua_dir, partners_json, skills_json, out_json = sys.argv[1:5]


def lua_row(text):
    """A Lua table literal such as {1, "a", {2, 3}} as a Python list."""
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
    text = re.sub(r",\s*\]", "]", "".join(out))  # a trailing comma, also one after a blank
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
    rows = {}
    for m in re.finditer(r'\n\t\[(\d+)(?:\.0)?\] = "((?:[^"\\]|\\.)*)",?', blk):
        rows[int(m.group(1))] = lua_row(m.group(2).replace('\\"', '"').replace("\\\\", "\\"))
    return rows


# dungeons: id, unit_id (index 12), power (index 13)
dungeons = table_rows(lua_dir + "/dungeon_data.lua", "Config.DungeonData.data_drama_dungeon_info_table")
# unit groups (data_unit1_table) and monsters (data_unit2_table) are spread over unit_data*.lua
groups, monsters = {}, {}
for f in sorted(glob.glob(lua_dir + "/unit_data[0-9]*.lua")):
    for name, dest in (("data_unit1_table", groups), ("data_unit2_table", monsters)):
        for m in re.finditer(r"Config\.UnitData\d*\." + name + " = \\{", open(f, encoding="utf-8").read()):
            pass
        src = open(f, encoding="utf-8").read()
        for m in re.finditer(r"(Config\.UnitData\d*\." + name + ")", src):
            dest.update(table_rows(f, m.group(1)))
            break

partners = json.load(open(partners_json, encoding="utf-8"))["partners"]
skills = json.load(open(skills_json, encoding="utf-8"))["skills"]


def monster_skills(head):
    p = partners.get(str(head))
    if not p or not p["stars"]:
        return None
    star = sorted(p["stars"], key=int)[0]
    return [sk[1] for sk in p["stars"][star].get("skills", []) if skills.get(str(sk[1]), {}).get("type") == "active_skill"]


out, no_group, no_monster, no_template, total = {}, 0, 0, 0, 0
for dun, r in sorted(dungeons.items()):
    unit_id, power = r[12], r[13]
    g = groups.get(unit_id)
    if not g:
        no_group += 1
        continue
    mons = []
    for mid in g[11:16]:  # monster1..monster5
        if not mid:
            continue
        m = monsters.get(mid)
        if not m:
            no_monster += 1
            continue
        total += 1
        sk = monster_skills(m[12])
        if sk is None:
            no_template += 1
        mons.append({"bid": mid, "name": m[1], "career": m[2], "camp": m[3], "lev": m[5], "head": m[12], "skills": sk or []})
    out[dun] = {"name": r[0], "unit_id": unit_id, "group": g[1], "power": power, "monsters": mons}

json.dump({"source": "restored client dungeon_data.lua + unit_data*.lua (+ partner skills); hp / atk / speed are not in the client tables",
           "dungeons": {str(k): v for k, v in out.items()}},
          open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out), "dungeons;", no_group, "without a unit group;", total, "monsters;", no_monster, "monster rows missing;", no_template, "without a head template in partners.json")
