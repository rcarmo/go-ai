// bun scripts/generate-durable-truncate-fixture.ts /path/to/official-v1.0.4
import {execFileSync} from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
const revision='7c10bd4337495ee613f2224843ecdf349b80d1df';
const checkout=path.resolve(process.argv[2]??'');
if(!process.argv[2]||execFileSync('git',['-C',checkout,'rev-parse','HEAD'],{encoding:'utf8'}).trim()!==revision)throw Error('Expected official v1.0.4 checkout');
const relative='packages/durable/src/truncate.ts';
if(fs.readFileSync(path.join(checkout,relative),'utf8')!==execFileSync('git',['-C',checkout,'show',revision+':'+relative],{encoding:'utf8'}))throw Error('Reference truncator has local edits');
const {truncateHead,truncateHeadOf}=await import(path.join(checkout,relative));
const cases:any[]=[];
for(const maxLines of [0,1,2,4])for(const maxBytes of [0,1,8,16])for(const whole of ['', '\n', 'a\nb\nc\nd\nend', '€\n\ufeffsecond\nlast\n', 'x'.repeat(32)+'\ntail']){
 const options={maxLines,maxBytes};let prefix='';
 for(const c of whole){prefix+=c;if(new TextEncoder().encode(prefix).length>maxBytes+1||[...prefix].filter(c=>c==='\n').length>=maxLines)break;}
 if(maxLines===0)prefix=whole;
 const complete=truncateHead(whole,options);
 const totals={lines:complete.totalLines,bytes:complete.totalBytes};
 const result=truncateHeadOf(prefix,totals,options);
 if(JSON.stringify(result)!==JSON.stringify(complete))throw Error('Insufficient reference prefix '+JSON.stringify({whole,prefix,options,result,complete}));
 cases.push({prefix,totals,options,result});
}
fs.writeFileSync(path.join(import.meta.dir,'../durable/tools/testdata/truncate-head-104.json'),JSON.stringify({revision,cases}));
console.log(cases.length+' pinned prefix/totals truncation fixtures');
