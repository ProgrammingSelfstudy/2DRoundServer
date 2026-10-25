#!/usr/bin/env python3
"""Extract the star tower ("海底监狱") from the restored client: the 4000 floors (name, recommended power, rewards
for the first win and for every win, and the monsters of the floor's unit group), the milestone rewards (every 10th
floor), the daily buy prices and how many a VIP level may buy.

usage: extract_star_tower.py LUA_DIR UNIT_DIR OUT_JSON
  LUA_DIR   .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW   (star_tower_data.lua)
  UNIT_DIR  .../outputs/lua-restored-paths/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW    (unit_data*.lua)
Needs a LuaJIT on the PATH.
"""
import glob, json, re, subprocess, sys

lua_dir, unit_dir, out_json = sys.argv[1:4]

LUA = r'''
Config = {}
dofile(arg[1] .. "/star_tower_data.lua")
local d = Config.StarTowerData
local function arr(t) local o = {} for _, e in ipairs(t or {}) do if type(e) == "table" then local r = {} for _, x in ipairs(e) do r[#r + 1] = tostring(x) end o[#o + 1] = "[" .. table.concat(r, ",") .. "]" else o[#o + 1] = tostring(e) end end return "[" .. table.concat(o, ",") .. "]" end
local c = d.data_tower_const
local floors = {}
for _, v in ipairs(d.data_tower_base) do
  floors[#floors + 1] = string.format('{"lev":%d,"name":%q,"unit_id":%d,"recommend":%d,"first":%s,"first2":%s,"award":%s}', v.lev, v.name, v.unit_id, v.recommend, arr(v.first_award), arr(v.first_award2), arr(v.award))
end
local miles = {}
for _, v in ipairs(d.data_get_floor_award) do miles[#miles + 1] = string.format('{"id":%d,"tower":%d,"award":%s}', v.id, v.tower, arr(v.award)) end
local buys = {}
for _, v in ipairs(d.data_tower_buy) do buys[#buys + 1] = string.format('{"id":%d,"expend":%s}', v.id, arr(v.expend)) end
local vip = {}
for i = 0, 40 do local v = d.data_tower_vip[i] if v then vip[#vip + 1] = tostring(v.buy_count) end end
io.write(string.format('{"free":%d,"open_lev":%d,"floors":[%s],"milestones":[%s],"buys":[%s],"vip_buys":[%s]}',
  c.free_times.val, c.open_floor.val[1][2], table.concat(floors, ","), table.concat(miles, ","), table.concat(buys, ","), table.concat(vip, ",")))
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

missing = 0
for fl in d["floors"]:
    g = groups.get(fl["unit_id"])
    mons = []
    for mid in (g or [])[11:16]:
        m = monsters.get(mid) if mid else None
        if m:
            mons.append([mid, m[1], m[2], m[3], m[5], str(m[12])])  # bid, name, career, camp, lev, head
    if not mons:
        missing += 1
    fl["monsters"] = mons
d["source"] = "restored client star_tower_data.lua + unit_data*.lua"
json.dump(d, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(d["floors"]), "floors;", len(d["milestones"]), "milestones;", missing, "floors without monsters")
