// Only marked disposable SQLite fixtures; never prints passwords or tokens.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
const file = process.env.ADMIN_UI_FIXTURE;
assert.ok(file, 'Set ADMIN_UI_FIXTURE to an isolated fixture.json');
const root = resolve(dirname(file));
assert.equal(await readFile(join(root,'.admin-ui-sandbox'),'utf8'),'2dround-admin-sandbox\n');
const config = JSON.parse(await readFile(join(root,'config.yaml'),'utf8'));
assert.equal(resolve(config.db_path),join(root,'admin.db'));
const fixture = JSON.parse(await readFile(file,'utf8'));
assert.equal(new URL(fixture.api).hostname,'127.0.0.1');
assert.equal(fixture.api,`http://${config.admin.addr}`);
const rid = fixture.rid;
function sql(action) {
 const result = spawnSync('python3',['-c',`
import sqlite3,json,sys,time
path,action,rid=sys.argv[1],sys.argv[2],int(sys.argv[3])
db=sqlite3.connect(path)
if action=='seed':
 for key in ('monthcard','daycharge','firstcharge_first'):
  db.execute('INSERT OR REPLACE INTO module_state(rid,module,data) VALUES(?,?,?)',(rid,key,'{}'))
 for i in range(105):
  db.execute('INSERT OR IGNORE INTO pay_orders(order_no,rid,srv_id,charge_id,amount_cents,status,created_at) VALUES(?,?,?,?,?,?,?)',('stage5_'+str(i),rid,'uitest_1',1,100,i%3,int(time.time())+i))
 db.commit()
else:
 out={}
 for (table,) in db.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name").fetchall():
  # The real gateway may persist native global timers independently of the UI.
  if table=='global_state': continue
  out[table]=sorted([repr(row) for row in db.execute('SELECT * FROM "'+table+'"')])
 print(json.dumps(out,sort_keys=True))
`,config.db_path,action,String(rid)],{encoding:'utf8'});
 assert.equal(result.status,0,'isolated database operation');
 return action === 'seed' ? undefined : JSON.parse(result.stdout);
}
async function call(path, token, method='GET', body, status=200) {
 const response = await fetch(fixture.api+'/api'+path,{method,headers:{...(token?{Authorization:`Bearer ${token}`} : {}),...(body?{'Content-Type':'application/json'} : {})},body:body ? JSON.stringify(body) : undefined,signal:AbortSignal.timeout(15000)});
 assert.equal(response.status,status,`${method} ${path}`);
 return response.json();
}
const tokens = {};
for(const role of ['viewer','operator','superadmin']) { const {username,password} = fixture.users[role]; tokens[role] = (await call('/login',undefined,'POST',{username,password})).token; }
sql('seed');
const before = sql('snapshot');
await call('/modules',undefined,'GET',undefined,401);
const registry = await call('/modules',tokens.viewer);
assert.equal(registry.items.length,108);
assert.ok(registry.items.every(m=>!m.editable));
const tables = (await call('/tables',tokens.viewer)).items;
assert.equal(tables.length,82);
const guild = (await call('/guilds?srv_id=uitest_1',tokens.viewer)).items[0];
assert.ok(guild,'seed isolated guilds with tools/adminmanagementfixture');
for(const role of ['viewer','operator','superadmin']) {
 const token = tokens[role];
 await call(`/roles/${rid}/modules`,token);
 for(const key of ['monthcard','daycharge','firstcharge_first','activity','cyclegift']) await call(`/roles/${rid}/modules/${key}`,token);
 const charge1 = await call(`/roles/${rid}/charge?limit=50&offset=0`,token);
 const charge2 = await call(`/roles/${rid}/charge?limit=50&offset=50`,token);
 assert.equal(charge1.modules.length,9); assert.equal(charge1.orders.total,105);
 assert.equal(charge1.orders.items.length,50); assert.equal(charge2.orders.items.length,50);
 assert.ok(!charge1.orders.items.some(a=>charge2.orders.items.some(b=>a.order_no === b.order_no)));
 assert.ok(charge2.orders.items.every(o=>o.rid === rid));
 await call(`/roles/${rid}/tables`,token);
 for(const t of tables) {
  const owner = t.scope === 'role' ? rid : t.scope === 'guild' ? guild.gid : 'uitest_1';
  const path = `/${{role:'roles',guild:'guilds',server:'servers'}[t.scope]}/${owner}/tables/${t.name}`;
  const page = await call(path+'?limit=50&offset=0',token);
  assert.ok(Array.isArray(page.rows)); assert.ok(page.total >= page.rows.length);
  for(const row of page.rows) assert.ok(Object.keys(row).every(c=>c === 'truncated' || t.columns.includes(c)));
 }
 await call('/servers/uitest_1/overview',token);
 const snapshots = await call('/servers/uitest_1/modules',token);
 assert.ok(snapshots.items.length >= 29);
 for(const m of registry.items.filter(m=>m.scope === 'global' && m.has_rank)) await call(`/servers/uitest_1/modules/${m.key}/rank?limit=200`,token);
 for(const m of snapshots.items.filter(m=>m.view?.season?.supported)) await call(`/servers/uitest_1/modules/${m.key}/season`,token);
 if(role !== 'superadmin') {
  await call(`/roles/${rid}/modules/monthcard/reset`,token,'POST',{item:'0',reason:'隔离权限验证'},403);
  await call('/servers/uitest_1/worldlev',token,'PUT',{override:1,until:0,reason:'隔离权限验证'},403);
  await call('/servers/uitest_1/seasons/arenateam/settle',token,'POST',{reason:'隔离权限验证'},403);
 }
}
assert.deepEqual(sql('snapshot'),before,'all non-global tables unchanged by reads and forbidden writes');
for(const [key,item] of [['monthcard','0'],['daycharge','today'],['firstcharge_first','all']]) await call(`/roles/${rid}/modules/${key}/reset`,tokens.superadmin,'POST',{item,reason:'阶段5隔离重置验证'});
await call(`/roles/${rid}/modules/monthcard/reset`,tokens.superadmin,'POST',{item:'all',reason:'非法项验证'},422);
await call(`/roles/${rid}/modules/monthcard/reset`,tokens.superadmin,'POST',{item:'0',reason:''},422);
const charge = await call(`/roles/${rid}/charge`,tokens.superadmin);
assert.equal(charge.modules.find(m=>m.key === 'firstcharge_first').exists,false);
const oldWorld = await call('/servers/uitest_1/worldlev',tokens.superadmin);
try {
 const world = await call('/servers/uitest_1/worldlev',tokens.superadmin,'PUT',{override:7,until:0,reason:'阶段5隔离覆盖验证'});
 assert.equal(world.world_level.effective,7); assert.equal(typeof world.push_errors,'number');
 const first = await call('/servers/uitest_1/seasons/arenateam/settle',tokens.superadmin,'POST',{reason:'阶段5隔离补结算验证'});
 const second = await call('/servers/uitest_1/seasons/arenateam/settle',tokens.superadmin,'POST',{reason:'阶段5隔离重复补结算验证'});
 assert.ok(['settled','already','not_due'].includes(first.reason)); assert.ok(['already','not_due'].includes(second.reason));
} finally {await call('/servers/uitest_1/worldlev',tokens.superadmin,'PUT',{override:oldWorld.override,until:oldWorld.until,reason:'恢复隔离覆盖值'});}
await call(`/roles/${rid}/tables/partners?limit=201`,tokens.viewer,'GET',undefined,422);
await call(`/roles/${rid}/tables/admins`,tokens.viewer,'GET',undefined,404);
console.log('Stage 5 integration passed: 3 roles, 82 table paths, module/charge pagination, 16 ranks, seasons, reset whitelist, world level and repeated settlement. Only disposable fixtures were changed.');
