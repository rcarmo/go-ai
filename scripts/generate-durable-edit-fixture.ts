import fs from 'node:fs';import path from 'node:path';import {execFileSync} from 'node:child_process';import {plugin} from 'bun';
const revision='a13d35a742c6ef8462812a28fbe1d8c8b7431c32';const checkout=path.resolve(process.argv[2]??'');const diffPackage=path.resolve(process.argv[3]??'');
if(!process.argv[2]||!process.argv[3]||execFileSync('git',['-C',checkout,'rev-parse','HEAD'],{encoding:'utf8'}).trim()!==revision)throw Error('Expected pinned checkout and diff8.0.4 directory');
const relative='packages/durable/src/tools/edit-diff.ts';if(fs.readFileSync(path.join(checkout,relative),'utf8')!==execFileSync('git',['-C',checkout,'show',revision+':'+relative],{encoding:'utf8'}))throw Error('Upstream helper has local edits');
const pkg=JSON.parse(fs.readFileSync(path.join(diffPackage,'package.json'),'utf8'));if(pkg.version!=='8.0.4')throw Error('Expected diff8.0.4');
plugin({name:'pinned-diff',setup(build){build.onResolve({filter:/^diff$/},()=>({path:path.join(diffPackage,'libesm/index.js')}))}});
const helper=await import(path.join(checkout,relative));
const inputs=[
 {content:'alpha\nbeta\ngamma\ndelta\n',edits:[{oldText:'alpha',newText:'ALPHA'},{oldText:'gamma',newText:'GAMMA'}]},
 {content:'untouched “smart”   \nfix “quote” – here  \ntail café  \n',edits:[{oldText:'fix "quote" - here',newText:'fixed'}]},
 {content:'head\nＣＡＦÉ\ntail\n',edits:[{oldText:'CAFÉ',newText:'coffee'}]},
 {content:'head “stay”  \nexact\nfuzzy “quote”\ntail “stay”  \n',edits:[{oldText:'exact',newText:'EXACT'},{oldText:'fuzzy "quote"',newText:'changed'}]},
 {content:'same “quote”\nsame "quote"\n',edits:[{oldText:'same "quote"',newText:'x'}]},
 {content:'abc',edits:[{oldText:'abc',newText:'ABC'},{oldText:'bc',newText:'x'}]},
 {content:'unchanged',edits:[{oldText:'unchanged',newText:'unchanged'}]},
 {content:'head\né spaces\u00a0x\ntail\n',edits:[{oldText:'é spaces x',newText:'updated\ninserted'}]},
 {content:'a\nb\nc\n',edits:[{oldText:'a\nb\n',newText:'a\nnew\n'}]},
 {content:'x\n'.repeat(20)+'old\n'+'y\n'.repeat(20),edits:[{oldText:'old',newText:'new'}]},
 {content:'line end\n',edits:[{oldText:'line end\n',newText:'last'}]},
 {content:'missing',edits:[{oldText:'absent',newText:'x'}]},
];
let seed=0x12345678;const random=()=>{seed=(Math.imul(seed,1664525)+1013904223)>>>0;return seed;};
for(let index=0;index<100;index++){const lines=[];for(let i=0,n=1+random()%30;i<n;i++)lines.push(['alpha','beta','repeat','repeat','中','é'][random()%6]);lines.splice(random()%(lines.length+1),0,'unique-'+index);const old='unique-'+index;const replacement=index%3===0?'added\nextra':index%3===1?'':'changed';inputs.push({content:lines.join('\n')+(index%2===0?'\n':''),edits:[{oldText:old,newText:replacement}]})}
for(let index=0;index<100;index++){const before=[],after=[];for(let i=0,n=1+random()%20;i<n;i++)before.push(['alpha','beta','repeat','repeat','中','é'][random()%6]);for(let i=0,n=1+random()%20;i<n;i++)after.push(['alpha','beta','repeat','repeat','中','é'][random()%6]);const old=before.join('\n')+'\n',replacement=after.join('\n')+'\n';if(old!==replacement)inputs.push({content:old,edits:[{oldText:old,newText:replacement}]})}
const cases=inputs.map(input=>{try{const result=helper.applyEditsToNormalizedContent(input.content,input.edits,'file');const display=helper.generateDiffString(result.baseContent,result.newContent);return {...input,result,display,patch:helper.generateUnifiedPatch('file',result.baseContent,result.newContent)}}catch(error){return {...input,error:String(error)}}});
fs.writeFileSync(path.join(import.meta.dir,'../durable/tools/testdata/edit-reference.json'),JSON.stringify({revision,diffVersion:pkg.version,cases}));console.log(cases.length+' pinned edit/diff cases');
