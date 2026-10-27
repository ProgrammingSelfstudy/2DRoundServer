#!/usr/bin/env python3
"""Extract the top-up products (charge_data.lua data_charge_data): price in yuan, diamonds, the extra for the first and the later
purchases, VIP experience, the first top-up gifts and the rewards of the accumulated top-up (by the VIP experience paid in), type (0 diamonds, 1 weekly gifts, 2 gifts and funds that the activities deliver).

usage: extract_charge.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
K = "Config.ChargeData."
m = dump(lua_dir, "charge_data.lua", [K + "data_charge_data", K + "data_charge_reward_data", K + "data_first_charge_data", K + "data_new_first_charge_data", K + "data_first_charge_lwt_data", K + "data_new_first_charge_data3"])
d = m[K + "data_charge_data"]
rows = list(d.values()) if isinstance(d, dict) else d
out = {"source": "restored client charge_data.lua",
       "products": [{"id": r["id"], "name": r["name"], "val": r["val"], "gold": r["gold"], "add": r["add"], "first_add": r["first_add"], "items": r["items"],
                     "vip_exp": r["vip_exp"], "type": r["type"], "sort": r["sort"], "pic": r["pic"]} for r in sorted(rows, key=lambda r: r["id"])]}
out["acc"] = [{"id": r["id"], "sum": r["charge_sum"], "items": r["item_list"]} for r in sorted(m[K + "data_charge_reward_data"], key=lambda r: r["id"])]
# the first top-up gifts (21000/21001 first, 21012~21014 first_new, 21030/21031 first_lwt, 21032/21033 first_3): fid 1 and 2 are the two tiers, each with three days (id 1~3 and 4~6)
for key, table in [("first", "data_first_charge_data"), ("first_new", "data_new_first_charge_data"), ("first_lwt", "data_first_charge_lwt_data"), ("first_3", "data_new_first_charge_data3")]:
    fd = m[K + table]
    out[key] = [{"id": r["id"], "fid": r["fid"], "day": r["day"], "need_exp": r["need_exp"], "items": r["item_list"]}
                for r in sorted(fd.values() if isinstance(fd, dict) else fd, key=lambda r: r["id"])]
# the weekly (gift_type 1) and monthly (2) gifts of the welfare window: what can be bought how often, and what each pays by the role's age
mm = dump(lua_dir, "misc_data.lua", ["Config.MiscData.data_cycle_gift_info", "Config.MiscData.data_cycle_gift_reward"])
out["cycle"] = {
    "info": [{"type": i + 1, "charge_id": r["charge_id"], "limit": r["limit_count"], "sort": r["sort_id"]}
             for i, grp in enumerate(mm["Config.MiscData.data_cycle_gift_info"]) for r in sorted(grp.values(), key=lambda r: r["charge_id"])],
    "reward": [{"charge_id": r["charge_id"], "min": r["min"], "max": r["max"], "reg_min": r["reg_min"], "reg_max": r["reg_max"], "sort": r["sort_id"], "items": r["reward"]}
               for k, rows in sorted(mm["Config.MiscData.data_cycle_gift_reward"].items(), key=lambda kv: int(kv[0])) for r in rows],
}
# the month funds (24700~24702): the two funds with the charge they are bought by, and the 30 days of rewards of each reward group
mf = dump(lua_dir, "month_fund_data.lua", ["Config.MonthFundData.data_fund_data", "Config.MonthFundData.data_fund_award"])
out["month_fund"] = {
    "funds": [{"id": r["id"], "charge_id": r["charge_id"], "name": r["name"]} for r in sorted(mf["Config.MonthFundData.data_fund_data"].values(), key=lambda r: r["id"])],
    "award": mf["Config.MonthFundData.data_fund_award"],
}
# the limited gifts (21210/21211): the client finds one by its place in the table, which is its id
sg = dump(lua_dir, "star_gift_data.lua", ["Config.StarGiftData.data_limit_gift_table"])["Config.StarGiftData.data_limit_gift_table"]
out["limit_gifts"] = [{"pos": i + 1, "id": r[0], "name": r[1], "kind": r[2], "cond": r[3], "items": r[4], "charge_id": r[5], "secs": r[6]} for i, r in enumerate(sg)]
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print("charge:", len(out["products"]), "products")
