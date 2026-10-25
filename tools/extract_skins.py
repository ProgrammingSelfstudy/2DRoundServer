#!/usr/bin/env python3
"""Extract the hero skins from the restored client's partner_skin_data.lua: per skin the hero
(bid) it belongs to, its name, level, the skin it upgrades to and what the upgrade costs.

usage: extract_skins.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
Needs a LuaJIT on the PATH (the file is plain Lua tables).
"""
import json, subprocess, sys

LUA = r'''
Config = {}
dofile(arg[1])
local out = {}
for id, v in pairs(Config.PartnerSkinData.data_skin_info) do
  local loss = {}
  for _, e in ipairs(v.upgrade_loss or {}) do loss[#loss + 1] = string.format("[%d,%d]", e[1], e[2]) end
  out[#out + 1] = string.format('"%d":{"bid":%d,"name":%q,"level":%d,"next":%d,"loss":[%s]}', id, v.bid, v.skin_name, v.level, v.next_id or 0, table.concat(loss, ","))
end
io.write("{" .. table.concat(out, ",") .. "}")
'''
raw = subprocess.run(["luajit", "-e", "arg={[1]=%r} " % (sys.argv[1] + "/partner_skin_data.lua") + LUA],
                     capture_output=True, text=True, check=True).stdout
skins = json.loads(raw)
json.dump({"source": "restored client partner_skin_data.lua (data_skin_info)", "skins": dict(sorted(skins.items(), key=lambda kv: int(kv[0])))},
          open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(skins), "skins;", sum(1 for v in skins.values() if v["next"]), "with an upgrade;", len({v["bid"] for v in skins.values()}), "heroes")
