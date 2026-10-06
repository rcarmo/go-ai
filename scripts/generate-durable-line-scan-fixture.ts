// bun scripts/generate-durable-line-scan-fixture.ts /path/to/official-v1.0.4
import {execFileSync} from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
const revision='7c10bd4337495ee613f2224843ecdf349b80d1df';
const checkout=path.resolve(process.argv[2]??'');
if(!process.argv[2]||execFileSync('git',['-C',checkout,'rev-parse','HEAD'],{encoding:'utf8'}).trim()!==revision)throw Error('Expected official v1.0.4 checkout');
const relative='packages/durable/src/env/line-scan.ts';
if(fs.readFileSync(path.join(checkout,relative),'utf8')!==execFileSync('git',['-C',checkout,'show',revision+':'+relative],{encoding:'utf8'}))throw Error('Reference line scanner has local edits');
const {LineScanner}=await import(path.join(checkout,relative));
const cases:any[]=[];let seed=1;const next=()=>{seed=(Math.imul(seed,1664525)+1013904223)>>>0;return seed};
for(let n=0;n<240;n++){
 const bytes=Array.from({length:next()%110},()=>[0,10,13,0xef,0xbb,0xbf,0xe2,0x82,0xac,0xf0,0x9f,0xff,65,0x80][next()%14]);
 if(n%4===0)bytes.unshift(0xef,0xbb,0xbf);
 const lines=bytes.filter(x=>x===10).length+1,start=next()%(lines+3),end=n%2===0?start+1+next()%5:undefined;
 const scanner=new LineScanner(start,end);
 for(let p=0;p<bytes.length;p+=7)scanner.push(Uint8Array.from(bytes.slice(p,p+7)));
 cases.push({bytes,start,end,scan:scanner.finish()});
}
fs.writeFileSync(path.join(import.meta.dir,'../durable/tools/testdata/line-scan-104.json'),JSON.stringify({revision,cases}));
console.log(cases.length+' pinned line scan fixtures');
