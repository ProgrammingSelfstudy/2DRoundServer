// Real Go integration for mail/pay/activities. Requires a marked disposable sandbox.
// No external payment service, player database, password or token output.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { randomBytes } from 'node:crypto';
import { activityForm, activityPayload, activityState, mailPayload, mailFeedback, manualOrder, paymentFeedback } from '../web/admin/.test-dist/src/domain/operations.js';
const file = process.env.ADMIN_UI_FIXTURE;
assert.ok(file, 'Set ADMIN_UI_FIXTURE to a disposable fixture.json');
assert.equal(await readFile(join(dirname(file), '.admin-ui-sandbox'), 'utf8'), '2dround-admin-sandbox\n');
const fixture = JSON.parse(await readFile(file, 'utf8'));
const base = new URL(fixture.api); assert.equal(base.hostname, '127.0.0.1');
const cfg = JSON.parse(await readFile(join(dirname(file),'config.yaml'),'utf8'));
assert.equal(cfg.db_path, join(dirname(file),'admin.db'));
assert.equal(base.origin, `http://${cfg.admin.addr}`);
const tokens = {};
async function call(method, path, body, token, status = 200) {
 const headers = { Accept:'application/json' }; if (token) headers.Authorization='Bearer '+token;
 if (body !== undefined) headers['Content-Type']='application/json';
 const response = await fetch(new URL(path,base),{method,headers,body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(10000),redirect:'error'});
 assert.equal(response.status,status,`${method} ${path}: HTTP status`); return response.json();
}
for (const role of ['viewer','operator','superadmin']) {
 const user=fixture.users[role]; const login=await call('POST','/api/login',{username:user.username,password:user.password}); tokens[role]=login.token;
 assert.equal((await call('GET','/api/me',undefined,tokens[role])).role,role);
 for (const path of ['/api/mails?limit=15&offset=0','/api/pay/config','/api/pay/products','/api/pay/orders?status=0&limit=100','/api/activities?limit=25&offset=0']) await call('GET',path,undefined,tokens[role]);
}
const viewer=tokens.viewer, operator=tokens.operator, manager=tokens.superadmin;
await call('GET','/api/mails',undefined,undefined,401);
for (const [method,path] of [['POST','/api/mails'],['POST','/api/activities'],['PUT','/api/activities/1'],['DELETE','/api/activities/1']]) await call(method,path,method==='DELETE'?undefined:{},viewer,403);
for (const token of [viewer,operator]) {
 for (const [method,path] of [['PUT','/api/pay/config'],['POST','/api/pay/orders'],['POST','/api/pay/orders/test/confirm'],['POST','/api/pay/orders/test/cancel']]) await call(method,path,{},token,403);
}
const rid=fixture.rid; const meta=await call('GET','/api/meta',undefined,viewer);
const tag='UI-'+Date.now(); const form={scope:'direct',roles:`${rid}, 2 ${rid}`,srv_id:'uitest_1',subject:tag+' 定向',content:'测试正文，不是真实运营邮件',from_name:'测试GM',type:1,expiry:'days',expire_days:7,expire_at:''};
const body=mailPayload(form,[{key:1,kind:'asset',asset:'gold',num:10},{key:2,kind:'item',asset:'',item_id:10034,num:2}],meta.assets);
const direct=await call('POST','/api/mails',body,operator,201); assert.ok(direct.push_error); assert.equal(direct.pushed,null); assert.equal(mailFeedback(direct).type,'warning');
const broadcast=await call('POST','/api/mails',mailPayload({...form,scope:'broadcast',subject:tag+' 区服广播'},[],meta.assets),operator,201);
const global=await call('POST','/api/mails',mailPayload({...form,scope:'broadcast',srv_id:'',subject:tag+' 全服广播'},[],meta.assets),operator,201);
const history=await call('GET','/api/mails?limit=200&offset=0',undefined,viewer);
const sent=history.items.find(m=>m.id===direct.mail.id); assert.equal(sent.recipients,2); assert.equal(sent.claimed,0); assert.equal(sent.attachments.length,2); assert.ok(history.item_names[10034]);
for (const id of [broadcast.mail.id,global.mail.id]) {const m=history.items.find(m=>m.id===id); assert.equal(m.recipients,0,'broadcast remains lazy without player listing'); assert.equal(m.claimed,0);}
assert.equal(history.items.find(m=>m.id===broadcast.mail.id).srv_id,'uitest_1'); assert.equal(history.items.find(m=>m.id===global.mail.id).srv_id,'');
const first=await call('GET','/api/mails?limit=1&offset=0',undefined,viewer), second=await call('GET','/api/mails?limit=1&offset=1',undefined,viewer); assert.equal(first.total,second.total); assert.notEqual(first.items[0].id,second.items[0].id);
await call('POST','/api/mails',{...body,role_ids:[999999999999]},operator,422);
await call('POST','/api/mails',{...body,attachments:[{kind:'item',item_id:10034,num:0}]},operator,422);
await call('POST','/api/mails',{...body,type:2},operator,422);
assert.equal((await call('GET','/api/mails?limit=1',undefined,viewer)).total,history.total,'rejected mails were not stored');
const oldConfig=await call('GET','/api/pay/config',undefined,viewer);
const png='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=';
for (const key of ['qr_wechat','qr_alipay']) {await call('PUT','/api/pay/config',{key,value:png},manager); assert.equal((await call('GET','/api/pay/config',undefined,viewer))[key],png); await call('PUT','/api/pay/config',{key,value:''},manager);}
await call('PUT','/api/pay/config',{key:'notice',value:'仅限隔离库测试，非真实付款'},manager);
assert.equal((await call('GET','/api/pay/config',undefined,viewer)).notice,'仅限隔离库测试，非真实付款');
await call('PUT','/api/pay/config',{key:'qr_wechat',value:'https://example.invalid/not-an-image'},manager,422);
await call('PUT','/api/pay/config',{key:'notice',value:'字'.repeat(301)},manager,422);
for (const key of ['qr_wechat','qr_alipay','notice']) await call('PUT','/api/pay/config',{key,value:oldConfig[key]||''},manager);
const products=(await call('GET','/api/pay/products',undefined,viewer)).items; assert.ok(products.some(p=>p.id===4));
const gold=role=>role.assets.find(a=>a.label===3)?.val||0;
const before=await call('GET',`/api/roles/${rid}`,undefined,viewer);
const pending=await call('POST','/api/pay/orders',manualOrder(rid,4,false,'隔离测试待确认',products),manager,201);
assert.equal(pending.order.status,0); assert.equal(gold(await call('GET',`/api/roles/${rid}`,undefined,viewer)),gold(before));
const paid=await call('POST',`/api/pay/orders/${pending.order.order_no}/confirm`,{note:'隔离测试确认'},manager);
assert.equal(paid.order.status,1); assert.ok(paid.push_error); assert.equal(paymentFeedback(paid).type,'warning');
const after=await call('GET',`/api/roles/${rid}`,undefined,viewer); assert.ok(gold(after)>gold(before),'confirm delivered resources');
await call('POST',`/api/pay/orders/${pending.order.order_no}/confirm`,{},manager,409);
await call('POST',`/api/pay/orders/${pending.order.order_no}/cancel`,{},manager,409);
assert.equal(gold(await call('GET',`/api/roles/${rid}`,undefined,viewer)),gold(after),'duplicate confirm never delivered twice');
const cancel=await call('POST','/api/pay/orders',manualOrder(rid,3,false,'测试取消',products),manager,201);
await call('POST',`/api/pay/orders/${cancel.order.order_no}/cancel`,{note:'隔离测试取消'},manager);
await call('POST',`/api/pay/orders/${cancel.order.order_no}/cancel`,{},manager,409);
await call('POST',`/api/pay/orders/${cancel.order.order_no}/confirm`,{},manager,409);
assert.equal(gold(await call('GET',`/api/roles/${rid}`,undefined,viewer)),gold(after),'cancel did not deliver');
const immediate=await call('POST','/api/pay/orders',manualOrder(rid,3,true,'测试立即发货',products),manager,201); assert.equal(immediate.order.status,1); assert.ok(immediate.push_error);
assert.ok(gold(await call('GET',`/api/roles/${rid}`,undefined,viewer))>gold(after));
assert.ok((await call('GET','/api/pay/orders?status=1&limit=100',undefined,viewer)).items.some(o=>o.order_no===pending.order.order_no));
assert.ok((await call('GET','/api/pay/orders?status=2&limit=100',undefined,viewer)).items.some(o=>o.order_no===cancel.order.order_no));
await call('POST','/api/pay/orders',{rid,charge_id:999999999},manager,422);
await call('POST','/api/pay/orders',{rid,charge_id:101},manager,422);
const now=Math.floor(Date.now()/1000);
const spec={title:tag+' 活动',title2:'副标题',panel_type:16,ico:'保留图标',type_ico:2,top_banner:'保留横幅',bottom_alert:'保留底部',reward_title:'保留奖励标题',channel_ban:'保留渠道',rule_str:'规则',time_str:'时间说明',aim_title:'目标',sort_val:2,cli_type:0,cli_type_name:'图标名称',daily:true,aims:[{id:1,str:'每日登录',event:'evt_cli_loaded',target:1,rewards:[[3,100],[10034,1]]}]};
const original=await call('POST','/api/activities',{srv_id:'uitest_1',camp_id:900001,bid:900001,status:1,start_at:now-60,end_at:now+3600,spec},operator,201);
const edit=activityForm(original); edit.title=tag+' 已编辑'; const edited=await call('PUT',`/api/activities/${original.id}`,activityPayload(edit,original),operator);
for (const key of ['ico','type_ico','top_banner','bottom_alert','reward_title','channel_ban','aims']) assert.deepEqual(edited.spec[key],spec[key],`preserved spec.${key}`);
const copied=await call('POST','/api/activities',activityPayload(activityForm(edited),edited),operator,201);
assert.notEqual(copied.id,edited.id); assert.equal(copied.camp_id,edited.camp_id); assert.equal(copied.bid,edited.bid); assert.equal(copied.start_at,edited.start_at); assert.deepEqual(copied.spec,edited.spec);
await call('POST','/api/activities',{srv_id:'',camp_id:1,bid:1,status:1,start_at:now,end_at:now+1,spec:{...spec,future_unknown:{keep:true}}},operator,422);
await call('POST','/api/activities',{srv_id:'',camp_id:1,bid:1,status:1,start_at:now,end_at:now-1,spec},operator,422);
await call('POST','/api/activities',{srv_id:'',camp_id:1,bid:1,status:1,start_at:now,end_at:0,spec:{...spec,aims:[{id:1,str:'x',event:'',target:0,rewards:[[999999999,1]]}]}},operator,422);
const allRow=await call('POST','/api/activities',{srv_id:'',camp_id:900002,bid:900002,status:0,start_at:now,end_at:0,spec:{title:tag+' 全服',panel_type:16}},operator,201);
const filter=(await call('GET','/api/activities?srv_id=uitest_2&limit=200',undefined,viewer)).items;
assert.ok(filter.some(a=>a.id===allRow.id)); assert.ok(!filter.some(a=>a.id===original.id));
const p1=await call('GET','/api/activities?limit=1&offset=0',undefined,viewer),p2=await call('GET','/api/activities?limit=1&offset=1',undefined,viewer); assert.notEqual(p1.items[0].id,p2.items[0].id); assert.equal(p1.total,p2.total);
for (const a of [original,copied,allRow]) await call('DELETE',`/api/activities/${a.id}`,undefined,operator);
const states=[{name:'关闭',status:0,start_at:now-60,end_at:0},{name:'未开始',status:1,start_at:now+86400,end_at:0},{name:'进行中',status:1,start_at:now-60,end_at:now+86400},{name:'已结束',status:1,start_at:now-86400,end_at:now-60}];
for (const [index,s] of states.entries()) {const row=await call('POST','/api/activities',{srv_id:'',camp_id:910000+index,bid:910000+index,status:s.status,start_at:s.start_at,end_at:s.end_at,spec:{title:`隔离示例 · ${s.name}`,panel_type:16}},operator,201); assert.equal(activityState(row,now),s.name);}
// Disposable authorization probe: do not reset any of the user's three fixture credentials.
const probeCredentials={username:'ui_ops_'+randomBytes(4).toString('hex'),password:randomBytes(18).toString('hex')};
const probe=await call('POST','/api/admins',{...probeCredentials,role:'operator'},manager,201);
const probeToken=(await call('POST','/api/login',probeCredentials)).token;
await call('GET','/api/mails',undefined,probeToken);
await call('PATCH',`/api/admins/${fixture.users.operator.id}`,{role:'viewer'},manager);
assert.equal((await call('GET','/api/me',undefined,operator)).role,'viewer'); await call('POST','/api/mails',body,operator,403); await call('POST','/api/activities',{},operator,403);
await call('PATCH',`/api/admins/${fixture.users.operator.id}`,{role:'operator'},manager);
await call('GET','/api/mails',undefined,'invalid-test-token',401);
await call('PATCH',`/api/admins/${probe.id}`,{status:0},manager);
for (const path of ['/api/mails','/api/pay/orders','/api/activities']) await call('GET',path,undefined,probeToken,401);
console.log('PASS: mail direct/server/global, attachments, lazy recipient counts, paging; payment config/permissions/create/confirm/cancel/duplicate delivery; activity edit/copy/extensions/state/filter/delete; demotion/unauthenticated. All writes used the disposable database.');
