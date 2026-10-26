#!/usr/bin/env python3
"""Extract the return action (returnaction, holiday_return_new_data.lua): the constants, the welcome gift, the 7 sign-in days,
the 7 tasks (reward, event, target), the 100 entries of the summon pool (reward, must / must_not draw number, show flag)
and the shop (period 1 only: the table has one period).

usage: extract_returnaction.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
K = "Config.HolidayReturnNewData."
r = dump(lua_dir, "holiday_return_new_data.lua", [K + n for n in ("data_constant", "data_privilege", "data_gift", "data_signin", "data_task", "data_summon", "data_shop")])
r = {k[len(K):]: v for k, v in r.items()}
const = {k: v["val"] for k, v in r["data_constant"].items() if not k.startswith("tips")}
out = {
    "source": "restored client holiday_return_new_data.lua",
    "const": const,
    "privilege": {"holiday_day": r["data_privilege"][0]["holiday_day"], "open_cond": r["data_privilege"][0]["open_cond"]},
    "gift": r["data_gift"][0]["rewards"],
    "signin": [{"day": x["day"], "rewards": x["rewards"]} for x in sorted(r["data_signin"][0], key=lambda x: x["day"])],
    "tasks": [{"id": t["id"], "title": t["title"], "desc": t["desc"], "award": t["award"], "label": t["progress"][0]["cli_label"], "target": t["progress"][0]["target_val"]}
              for t in sorted(r["data_task"].values(), key=lambda t: t["id"])],
    "summon": [{"id": x["id"], "type_id": x["type_id"], "rewards": x["rewards"], "must": x["must"], "must_not": x["must_not"], "show": x["show"]}
               for x in sorted(r["data_summon"][0], key=lambda x: x["id"])],
    "shop": [{"id": x["id"], "award": x["award"], "expend": x["expend"], "limit_all": x["r_limit_all"], "limit_day": x["r_limit_day"]}
             for x in sorted(r["data_shop"][0], key=lambda x: x["id"])],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print("returnaction:", len(out["signin"]), "days;", len(out["tasks"]), "tasks;", len(out["summon"]), "pool entries;", len(out["shop"]), "shop rows")
