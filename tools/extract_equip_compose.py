#!/usr/bin/env python3
"""Extract the equipment compose chains of the forge house from the restored client's partner_eqm_data.lua
(data_eqm_compose_id): per equipment kind a list of steps, each making an item from 3 of the item before it and some coin.

usage: extract_equip_compose.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
Needs a LuaJIT on the PATH.
"""
import json, subprocess, sys

LUA = r'''
Config = {}
dofile(arg[1] .. "/partner_eqm_data.lua")
local out = {}
for kind, list in pairs(Config.PartnerEqmData.data_eqm_compose_id) do
  local steps = {}
  for _, e in ipairs(list) do
    steps[#steps + 1] = string.format('{"id":%d,"need":%d,"n":%d,"coin":%d}', e.id, e.expend[1][1], e.expend[1][2], e.expend[2][2])
  end
  out[#out + 1] = string.format('{"kind":%d,"steps":[%s]}', kind, table.concat(steps, ","))
end
io.write("[" .. table.concat(out, ",") .. "]")
'''
chains = json.loads(subprocess.run(["luajit", "-e", "arg={[1]=%r} " % sys.argv[1] + LUA], capture_output=True, text=True, check=True).stdout)
chains.sort(key=lambda c: c["kind"])
json.dump({"source": "restored client partner_eqm_data.lua data_eqm_compose_id", "chains": chains}, open(sys.argv[2], "w"), separators=(",", ":"))
print(len(chains), "chains;", sum(len(c["steps"]) for c in chains), "steps")
