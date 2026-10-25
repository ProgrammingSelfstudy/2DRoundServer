#!/usr/bin/env python3
"""Extract the hero talent ("天赋栏", dower skill) tables from the restored client's
partner_skill_data.lua: the slots and what opens them, the learnable skills with their
cost and career limit, the level-up chain and its cost, and the forget cost / refund.

usage: extract_talent.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
Needs a LuaJIT on the PATH (the file is plain Lua tables).
"""
import json, subprocess, sys

LUA = r'''
Config = {}
dofile(arg[1])
local d = Config.PartnerSkillData
local function enc(v)
  if type(v) == "table" then
    local n, mx = 0, 0
    for k in pairs(v) do n = n + 1; if type(k) == "number" and k > mx then mx = k end end
    if n == 0 then return "[]" end
    local parts = {}
    if mx == n then
      for i = 1, n do parts[#parts + 1] = enc(v[i]) end
      return "[" .. table.concat(parts, ",") .. "]"
    end
    for k, x in pairs(v) do parts[#parts + 1] = string.format("%q:%s", tostring(k), enc(x)) end
    return "{" .. table.concat(parts, ",") .. "}"
  elseif type(v) == "string" then return string.format("%q", v)
  else return tostring(v) end
end
io.write(enc({pos = d.data_partner_skill_pos, learn = d.data_partner_skill_learn, level = d.data_partner_skill_level,
  back = d.data_partner_skill_back, map = d.data_partner_skill_map}))
'''
raw = subprocess.run(["luajit", "-e", "arg={[1]=%r} " % (sys.argv[1] + "/partner_skill_data.lua") + LUA],
                     capture_output=True, text=True, check=True).stdout
d = json.loads(raw)
out = {
    "source": "restored client partner_skill_data.lua (pos, learn, level, map, back)",
    "pos": [{"pos": p["pos"], "star": p["pos_limit"][1]} for p in d["pos"]],
    "learn": {k: {"cost": v["expend"], "careers": [c[0] for c in v["limit_career"]]} for k, v in d["learn"].items()},
    "level": {k: {"next": v["next_id"], "cost": v["expend"]} for k, v in d["level"].items()},
    "prev": {k: v for k, v in d["map"].items()},
    "back": {k: {"cost": v["expend"], "award": v["award1"]} for k, v in d["back"].items()},
}
json.dump(out, open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out["pos"]), "slots;", len(out["learn"]), "learnable;", len(out["level"]), "level steps;", len(out["back"]), "forget rows")
