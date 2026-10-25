#!/usr/bin/env python3
"""Extract the guild boss rules from the restored client: the constants, the chapters (one boss each) with the
reward of a challenge (`dps_awrard`) and of a kill (`award`, guild exp), the damage ranking rewards per boss, the
buy-count prices and the attack buffs (guild_dun_data.lua), and for every boss its hp and the monster it fights
as (unit_data*.lua + the hero template's active skills).

usage: extract_guild_boss.py LUA_DIR PARTNERS_JSON SKILLS_JSON OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
Needs a LuaJIT on the PATH (the files are plain Lua tables).
"""
import json, subprocess, sys

lua_dir, partners_json, skills_json, out_json = sys.argv[1:5]

LUA = r'''
Config = {}
dofile(arg[1] .. "/guild_dun_data.lua")
local d = Config.GuildDunData
local function arr(t) local o = {} for _, e in ipairs(t or {}) do if type(e) == "table" then local r = {} for _, x in ipairs(e) do r[#r + 1] = tostring(x) end o[#o + 1] = "[" .. table.concat(r, ",") .. "]" else o[#o + 1] = tostring(e) end end return "[" .. table.concat(o, ",") .. "]" end
local consts = {}
for _, k in ipairs({"guild_lev","join_lev","free_time","action_time","rank_dam","buff_cost","buff_item","concentrate_cd","praise_time","box_max_num"}) do
  consts[#consts + 1] = string.format('"%s":%d', k, d.data_const[k].val)
end
local bosses = {}
for id, v in pairs(d.data_guildboss_list) do
  bosses[#bosses + 1] = string.format('{"boss_id":%d,"id":%d,"combat_id":%d,"hp":%d,"name":%q}', v.boss_id, v.id, v.combat_id, v.hp, v.item_name)
end
local chapters = {}
for i, v in ipairs(d.data_chapter_reward) do
  if d.data_chapter_boss[i] then chapters[#chapters + 1] = string.format('{"id":%d,"name":%q,"desc":%q,"show_id":%d,"guild_exp":%d,"challenge":%s,"kill":%s,"boss":%d}',
    v.id, v.chapter_name, v.chapter_desc, v.show_id, v.guild_exp, arr(v.dps_awrard), arr(v.award), d.data_chapter_boss[i][1].boss_id) end
end
local ranks = {}
for bid, list in pairs(d.data_rank_reward) do
  for _, v in ipairs(list) do ranks[#ranks + 1] = string.format('{"boss_id":%d,"rank1":%d,"rank2":%d,"award":%s}', bid, v.rank1, v.rank2, arr(v.award)) end
end
local buys = {}
for i, v in ipairs(d.data_buy_count) do buys[#buys + 1] = string.format('{"count":%d,"vip_lev":%d,"expend":%s}', v.count, v.vip_lev, arr(v.expend)) end
local buffs = {}
for i, v in ipairs(d.data_buff_data) do buffs[#buffs + 1] = string.format('{"lev":%d,"atk_per":%d}', v.buff_lev, v.add_attr[1][2]) end
io.write(string.format('{"const":{%s},"bosses":[%s],"chapters":[%s],"rank_rewards":[%s],"buys":[%s],"buffs":[%s]}',
  table.concat(consts, ","), table.concat(bosses, ","), table.concat(chapters, ","), table.concat(ranks, ","), table.concat(buys, ","), table.concat(buffs, ",")))
'''
d = json.loads(subprocess.run(["luajit", "-e", "arg={[1]=%r} " % lua_dir + LUA], capture_output=True, text=True, check=True).stdout)

# the monster each boss fights as; the unit tables keep their rows as strings, so read them the way extract_dungeon_units.py does
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


missing = 0
for b in d["bosses"]:
    g = groups.get(b["combat_id"])
    mid = next((x for x in (g or [])[11:16] if x), None)
    m = monsters.get(mid) if mid else None
    if not m:
        missing += 1
        b["monster"] = None
        continue
    b["monster"] = {"bid": mid, "name": m[1], "career": m[2], "camp": m[3], "lev": m[5], "head": m[12], "skills": monster_skills(m[12])}
d["source"] = "restored client guild_dun_data.lua + unit_data*.lua"
json.dump(d, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(d["bosses"]), "bosses;", len(d["chapters"]), "chapters;", len(d["rank_rewards"]), "rank reward rows;", missing, "bosses without a monster row")
