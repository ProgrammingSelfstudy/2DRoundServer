#!/usr/bin/env python3
"""Extract the guild secret area ("crusade") rules from the restored client: the constants, the 250 bosses
(five lines of 50 difficulties: hp range, vitality to open, time, the monster each fights as), the challenge
reward per boss, the stage chests per boss (progress in per mille of hp lost) and the damage ranking rewards.

usage: extract_guild_secret.py LUA_DIR PARTNERS_JSON SKILLS_JSON OUT_JSON UNIT_DIR
  LUA_DIR   .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW   (guild_secret_area_data.lua)
  UNIT_DIR  .../outputs/lua-restored-paths/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW    (unit_data*.lua; the other copy keeps them in another form)
Needs a LuaJIT on the PATH.
"""
import json, subprocess, sys

sys.path.insert(0, __file__.rsplit("/", 1)[0])
lua_dir, partners_json, skills_json, out_json, unit_dir = sys.argv[1:6]

LUA = r'''
Config = {}
dofile(arg[1] .. "/guild_secret_area_data.lua")
local d = Config.GuildSecretAreaData
local function arr(t) local o = {} for _, e in ipairs(t or {}) do if type(e) == "table" then local r = {} for _, x in ipairs(e) do r[#r + 1] = tostring(x) end o[#o + 1] = "[" .. table.concat(r, ",") .. "]" else o[#o + 1] = tostring(e) end end return "[" .. table.concat(o, ",") .. "]" end
local c = d.data_const
local ids = {}
for id in pairs(d.data_boss_info_table) do ids[#ids + 1] = id end
table.sort(ids)
local bosses, boxes, ranks = {}, {}, {}
for _, id in ipairs(ids) do
  local v = d.data_boss_info(id)
  local cr = d.data_chapter_reward(id)
  bosses[#bosses + 1] = string.format('{"id":%d,"pass_id":%d,"name":%q,"unit_id":%d,"open_lev":%d,"difficulty":%q,"time":%d,"active":%d,"hp":%s,"skills":%s,"challenge":%s}',
    v.id, v.pass_id, v.name, v.unit_id, v.open_lev, v.difficulty, v.time, v.active, arr(v.difficulty_change[1] and {v.difficulty_change[1]}), arr({}), arr(cr.change_reward))
  local bx = {}
  for _, b in ipairs(d.data_box_reward[id] or {}) do bx[#bx + 1] = string.format('{"number":%d,"progress":%d,"reward":%s}', b.number, b.progress, arr(b.reward)) end
  boxes[#boxes + 1] = string.format('"%d":[%s]', id, table.concat(bx, ","))
  local rk = {}
  for _, r in ipairs(d.data_rank_reward[id] or {}) do rk[#rk + 1] = string.format('{"min":%d,"max":%d,"reward":%s}', r.min, r.max, arr(r.reward)) end
  ranks[#ranks + 1] = string.format('"%d":[%s]', id, table.concat(rk, ","))
end
io.write(string.format('{"free":%d,"buy":%d,"init":%s,"reset_interval":%d,"boss_interval":%d,"cost":%s,"bosses":[%s],"boxes":{%s},"ranks":{%s}}',
  c.guild_secret_free_time.val, c.guild_secret_buy_time.val, arr(c.guild_secret_init_boss.val), c.player_reset_interval.val, c.boss_reset_interval.val, arr(d.data_cost_info[1]), table.concat(bosses, ","), table.concat(boxes, ","), table.concat(ranks, ",")))
'''
d = json.loads(subprocess.run(["luajit", "-e", "arg={[1]=%r} " % lua_dir + LUA], capture_output=True, text=True, check=True).stdout)

import re, glob


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
    rows = {}
    for m in re.finditer(r'\n\t\[(\d+)(?:\.0)?\] = "((?:[^"\\]|\\.)*)",?', blk):
        rows[int(m.group(1))] = lua_row(m.group(2).replace('\\"', '"').replace("\\\\", "\\"))
    return rows


groups, monsters = {}, {}
for f in sorted(glob.glob(unit_dir + "/unit_data[0-9]*.lua")):
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


missing = 0
for b in d["bosses"]:
    g = groups.get(b["unit_id"])
    mid = next((x for x in (g or [])[11:16] if x), None)
    m = monsters.get(mid) if mid else None
    if not m:
        missing += 1
        b["monster"] = None
    else:
        b["monster"] = {"bid": mid, "name": m[1], "career": m[2], "camp": m[3], "lev": m[5], "head": m[12], "skills": monster_skills(m[12])}
    b["hp"] = b["hp"][0] if b["hp"] else [1000000, 1000000]
    del b["skills"]
d["source"] = "restored client guild_secret_area_data.lua + unit_data*.lua"
json.dump(d, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(d["bosses"]), "bosses;", missing, "without a monster row")
