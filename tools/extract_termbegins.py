#!/usr/bin/env python3
"""Extract the school-term action (actiontermbegins, holiday_term_begins_data.lua): the constants, per round the chapters
(four difficulties of five levels: the recommended power, the monster, the reward) and the world boss, the paper collect
rewards, the damage rank rewards, and for every monster the unit row it fights as (unit_data*.lua + the hero template's
active skills, as extract_guild_boss.py does).

usage: extract_termbegins.py LUA_DIR PARTNERS_JSON SKILLS_JSON OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import glob, json, re, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, partners_json, skills_json, out_json = sys.argv[1:5]
K = "Config.HolidayTermBeginsData."
raw = dump(lua_dir, "holiday_term_begins_data.lua", [K + n for n in ("data_const", "data_chapter_info", "data_boss_info", "data_cellect_reward_info", "data_rank_reward", "data_max_chapter_id", "data_max_diff")])
raw = {k[len(K):]: v for k, v in raw.items()}


def rows(x):
    return list(x.values()) if isinstance(x, dict) else x


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


units, missing = {}, 0


def unit(uid):
    global missing
    if str(uid) in units:
        return
    g = groups.get(uid)
    mid = next((x for x in (g or [])[11:16] if x), None)
    m = monsters.get(mid) if mid else None
    if not m:
        missing += 1
        return
    units[str(uid)] = {"bid": mid, "name": m[1], "career": m[2], "camp": m[3], "lev": m[5], "head": m[12], "skills": monster_skills(m[12])}


const = {k: v["val"] for k, v in raw["data_const"].items() if not k.endswith("descreption")}
rounds = []
for ri, rd in enumerate(raw["data_chapter_info"]):
    chapters = []
    for dd in rd:
        for c in sorted(rows(dd), key=lambda c: c["order_id"]):
            unit(c["unit_id"])
            chapters.append({"order": c["order_id"], "diff": c["type"], "name": c["order_name"], "power": c["power"], "unit": c["unit_id"], "award": c["hit_award"]})
    bosses = sorted(rows(raw["data_boss_info"][ri]), key=lambda b: b["boss_id"])
    for b in bosses:
        unit(b["unit_id"])
    rounds.append({"chapters": chapters, "boss": {"id": bosses[0]["boss_id"], "name": bosses[0]["boss_name"], "unit": bosses[0]["unit_id"], "award": bosses[0]["hit_award"]},
                   "max_order": raw["data_max_chapter_id"][ri], "max_diff": raw["data_max_diff"][ri]})
out = {"source": "restored client holiday_term_begins_data.lua + unit_data*.lua", "const": const, "rounds": rounds, "units": units,
       "collect": sorted(raw["data_cellect_reward_info"], key=lambda r: r["id"]), "ranks": raw["data_rank_reward"]}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print("termbegins:", len(rounds), "rounds;", [len(r["chapters"]) for r in rounds], "chapters;", len(units), "units;", missing, "without a unit row")
