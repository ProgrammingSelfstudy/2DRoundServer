#!/usr/bin/env python3
"""Extract the level -> exp table from the restored client's role_data.lua
(Config.RoleData.data_role_attr). The server sends exp_max / exp_total_nextlev
in 10301 and must agree with the client's own table.

usage: extract_role_exp.py CLIENT_ROOT OUT_JSON
"""
import json, re, sys

src = open(sys.argv[1] + "/outputs/lua-restored-paths/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW/role_data.lua", encoding="utf-8").read()
declared = int(re.search(r"data_role_attr_length = (\d+)", src).group(1))
start = src.index("Config.RoleData.data_role_attr = {")
end = src.index("function Config.RoleData.data_role_attr_fun")
blocks = re.findall(r"\n\t\{\n(.*?)\n\t\},?(?=\n)", src[start:end], re.S)
assert len(blocks) == declared, (len(blocks), declared)
table = []
for i, b in enumerate(blocks):
    t, m = re.search(r"exp_total = (\d+),\s*exp_max = (\d+),", b).groups()
    rw = re.search(r"reward = \{(.*)\},?\s*$", b, re.S).group(1)
    # reward: what reaching this level pays, [asset or item id, amount] pairs
    table.append({"lev": i + 1, "exp_total": int(t), "exp_max": int(m),
                  "reward": [[int(a), int(c)] for a, c in re.findall(r"\{\s*(\d+),\s*(\d+),?\s*\}", rw)]})
# The client's exp_total column is not always the running sum of exp_max (level 24
# is wrong in the original data). Keep exp_max as the source of truth, compute
# the running totals ourselves, and record where the original disagrees.
running, anomalies = 0, []
for row in table:
    running += row["exp_max"]
    if row["exp_total"] != running:
        anomalies.append({"lev": row["lev"], "client_exp_total": row["exp_total"], "running_sum": running})
    row["exp_total"] = running
json.dump({"source": "restored client role_data.lua Config.RoleData.data_role_attr; exp_total recomputed as the running sum of exp_max",
           "anomalies_in_client_data": anomalies, "levels": table},
          open(sys.argv[2], "w"), ensure_ascii=False, indent=0)
print(len(table), "levels; lev1 =", table[0], "; lev200 =", table[-1])
print("client rows whose exp_total is not the running sum:", anomalies)
