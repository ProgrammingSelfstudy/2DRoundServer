#!/usr/bin/env python3
"""Extract the goddess trial boss (holiday_valentine_boss_data.lua): per camp the boss (the unit it fights as, the free
and bought fights, the guaranteed item), the prices of the bought fights, and the damage steps that give the item count;
for every unit the monster row it is made of (unit_data*.lua + the hero template's active skills, as extract_sandybeach.py).

usage: extract_whiteday.py LUA_DIR PARTNERS_JSON SKILLS_JSON OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import glob, json, re, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, partners_json, skills_json, out_json = sys.argv[1:5]
K = "Config.HolidayValentineBossData."
raw = dump(lua_dir, "holiday_valentine_boss_data.lua", [K + n for n in ("data_constant", "data_boss_list", "data_award_list")])
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


camps = {}
for camp, v in raw["data_boss_list"].items():
    for b in (v.values() if isinstance(v, dict) else v):
        unit(b["boss_id"])
        camps[str(camp)] = {"id": b["id"], "boss": b["boss_id"], "title": b["title_name"], "free": b["time"], "buy": b["buy_time"], "reward": b["reward"]}
# the decompiled list of the first boss has its first row twice ({...} and [1] = {...}), so the rows are read from the text
src = open(lua_dir + "/holiday_valentine_boss_data.lua", encoding="utf-8").read()
src = src[src.index("data_award_list = {"):]
steps = {}
for m in re.finditer(r"box_count = (\d+),\s*id = (\d+),\s*max = (\d+),\s*min = (\d+),", src):
    steps.setdefault(m.group(2), []).append({"min": int(m.group(4)), "max": int(m.group(3)), "count": int(m.group(1))})
steps = {k: sorted(v, key=lambda r: r["min"]) for k, v in steps.items()}
prices = [raw["data_constant"]["holiday_valentine_boss_buy_loss%d" % i]["val"][0] for i in range(1, 6)]
out = {"source": "restored client holiday_valentine_boss_data.lua + unit_data*.lua", "camps": camps, "steps": steps, "prices": prices, "units": units}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print("whiteday:", len(camps), "camps;", len(units), "units;", missing, "without a unit row")
