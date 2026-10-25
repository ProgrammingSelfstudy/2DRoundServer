#!/usr/bin/env python3
"""Extract the avatar frames (avatar_data.lua, data_avatar), the titles (honor_data.lua,
data_title) and the role models (looks_data.lua, data_data: the hero, star, level and skin a model
needs) of the restored client: per frame / title id the name, how long it lasts once activated
(expire_time, minutes; 0 = for good) and what activating it costs (loss, a list of {item, num}).

usage: extract_looks.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
Needs a LuaJIT on the PATH (the files are plain Lua tables).
"""
import json, subprocess, sys

LUA = r'''
Config = {}
dofile(arg[1]); dofile(arg[2]); dofile(arg[3])
local function dump(t)
  local out = {}
  for id, v in pairs(t) do
    local loss = {}
    for _, e in ipairs(v.loss or {}) do loss[#loss + 1] = string.format("[%d,%d]", e[1], e[2]) end
    out[#out + 1] = string.format('"%d":{"name":%q,"minutes":%d,"loss":[%s]}', id, v.name, v.expire_time, table.concat(loss, ","))
  end
  return "{" .. table.concat(out, ",") .. "}"
end
local models = {}
for id, v in pairs(Config.LooksData.data_data) do
  models[#models + 1] = string.format('"%d":{"name":%q,"partner":%d,"star":%d,"lev":%d,"skin":%d}', id, v.name, v.partner_id, v.star, v.lev, v.skin_id)
end
io.write(string.format('{"avatar":%s,"title":%s,"model":{%s}}', dump(Config.AvatarData.data_avatar), dump(Config.HonorData.data_title), table.concat(models, ",")))
'''
d = sys.argv[1]
raw = subprocess.run(["luajit", "-e", "arg={[1]=%r,[2]=%r,[3]=%r} " % (d + "/avatar_data.lua", d + "/honor_data.lua", d + "/looks_data.lua") + LUA],
                     capture_output=True, text=True, check=True).stdout
o = json.loads(raw)
o["source"] = "restored client avatar_data.lua (data_avatar), honor_data.lua (data_title)"
json.dump(o, open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(o["avatar"]), "avatar frames;", len(o["title"]), "titles;", len(o["model"]), "role models")
