#!/usr/bin/env python3
"""Extract the material dungeons ("材料副本", stone dungeons) from the restored client: the five kinds (coin,
experience, hero, artifact, rune) with nine difficulties each (unlock chain, recommended power, reward, the monsters
of the unit group), the daily attempts and what an extra one costs per VIP level, and the level each kind opens at.

usage: extract_stone_dungeon.py LUA_DIR UNIT_DIR OUT_JSON
  LUA_DIR   .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW   (dungeon_stone_data.lua)
  UNIT_DIR  .../outputs/lua-restored-paths/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW    (unit_data*.lua)
Needs a LuaJIT on the PATH.
"""
import glob, json, re, subprocess, sys

lua_dir, unit_dir, out_json = sys.argv[1:4]

LUA = r'''
Config = {}
dofile(arg[1] .. "/dungeon_stone_data.lua")
local d = Config.DungeonStoneData
local function arr(t) local o = {} for _, e in ipairs(t or {}) do if type(e) == "table" then local r = {} for _, x in ipairs(e) do r[#r + 1] = tostring(x) end o[#o + 1] = "[" .. table.concat(r, ",") .. "]" else o[#o + 1] = tostring(e) end end return "[" .. table.concat(o, ",") .. "]" end
local types = arr(d.data_const.dungeon_type.val)
local duns = {}
for _, group in pairs(d.data_award_list) do
  for id, v in pairs(group) do
    duns[#duns + 1] = string.format('{"id":%d,"type":%d,"difficulty":%d,"lev_limit":%d,"pre_id":%d,"power":%d,"unit_id":%d,"name":%q,"title":%q,"first":%s,"show":%s}',
      v.id, v.dun_type, v.difficulty, v.lev_limit, v.pre_id, v.power, v.unit_id, v.name, v.title_name, arr(v.first_items), arr(v.show_items))
  end
end
local buys = {}
for t, list in pairs(d.data_buy) do
  local o = {}
  for _, e in ipairs(list) do o[#o + 1] = string.format('{"cost":%d,"vip":%d}', e.cost, e.vip) end
  buys[#buys + 1] = string.format('"%d":[%s]', t, table.concat(o, ","))
end
local opens = {}
for t, v in pairs(d.data_type_open) do opens[#opens + 1] = string.format('"%d":%d', v.id, v.activate[1][2]) end
io.write(string.format('{"free":%d,"hook_cost":%d,"types":%s,"dungeons":[%s],"buys":{%s},"open_lev":{%s}}',
  d.data_const.free_num.val, d.data_const.hook_cost.val, types, table.concat(duns, ","), table.concat(buys, ","), table.concat(opens, ",")))
'''
d = json.loads(subprocess.run(["luajit", "-e", "arg={[1]=%r} " % lua_dir + LUA], capture_output=True, text=True, check=True).stdout)


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

d["dungeons"].sort(key=lambda e: e["id"])
missing = 0
for dun in d["dungeons"]:
    g = groups.get(dun["unit_id"])
    mons = []
    for mid in (g or [])[11:16]:
        m = monsters.get(mid) if mid else None
        if m:
            mons.append([mid, m[1], m[2], m[3], m[5], str(m[12])])  # bid, name, career, camp, lev, head
    if not mons:
        missing += 1
    dun["monsters"] = mons
d["source"] = "restored client dungeon_stone_data.lua + unit_data*.lua"
json.dump(d, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(d["dungeons"]), "dungeons;", missing, "without monsters")
