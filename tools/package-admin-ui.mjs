// Package only the reproducible Vite production output for go:embed.
import {readFile,stat,readdir,mkdir,cp,rm,rename} from 'node:fs/promises';
import {resolve,join,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
const repo=resolve(dirname(fileURLToPath(import.meta.url)),'..'),source=join(repo,'web/admin/dist'),target=join(repo,'internal/admin/ui'),stage=target+'.staging';
const index=await readFile(join(source,'index.html'),'utf8');
if(!index.includes('<div id="app"></div>') || !/src="\/assets\/[^"\s]+\.js"/.test(index) || /(?:src|href)="(?:https?:)?\/\//.test(index))throw Error('Expected self-contained Vite production index');
for(const match of index.matchAll(/(?:src|href)="(\/assets\/[^"\s]+)"/g))await stat(join(source,match[1]));
const assets=await readdir(join(source,'assets'));if(!assets.some(n=>n.endsWith('.js'))||!assets.some(n=>n.endsWith('.css')))throw Error('Missing production assets');
await rm(stage,{recursive:true,force:true});await mkdir(stage,{recursive:true});
await cp(join(source,'index.html'),join(stage,'index.html'));await cp(join(source,'assets'),join(stage,'assets'),{recursive:true});
// Nothing from node_modules, .env, source maps or external checkout paths is embedded.
await rm(target,{recursive:true,force:true});await rename(stage,target);
console.log(`Packaged Vite index and ${assets.length} assets for Go embed`);
