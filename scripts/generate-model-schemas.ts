// Generate shared model/compat schemas only from official pi-ai1.4.0 and its
// pinned TypeBox1.3.27 dependency. Requires source and TypeBox installed alongside
// that source beneath the resolved project root; no global npm install.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
const source=process.env.PI_AI_SCHEMA_SOURCE;
if(!source||!path.isAbsolute(source))throw Error('PI_AI_SCHEMA_SOURCE must name the absolute pinned source root');
const pkg=JSON.parse(fs.readFileSync(path.join(source,'packages/ai/package.json'),'utf8'));
if(pkg.version!=='1.4.0'||pkg.dependencies.typebox!=='1.3.27')throw Error('incorrect schema package/dependency pin');
const pinned={
 'model-schema.ts':'1229ca9e0b4489d8d5bd095788608d5c86f42a6e2a66e75f504d0045d05cc671',
 'compat-schema.ts':'b65a1a7c2981eec921c396348bf12b96ca797a8cbfb3e540eed6c4200c83c1ff'
};
for(const [file,expected] of Object.entries(pinned)){
 const actual=crypto.createHash('sha256').update(fs.readFileSync(path.join(source,'packages/ai/src/providers',file))).digest('hex');
 if(actual!==expected)throw Error(`schema source differs from exact official1.4.0: ${file}`);
}
const model=await import(path.join(source,'packages/ai/src/providers/model-schema.ts'));
const compat=await import(path.join(source,'packages/ai/src/providers/compat-schema.ts'));
const schemas:Record<string,unknown>={};
for(const module of [model,compat])for(const [name,value] of Object.entries(module)){if(name.endsWith('Schema')&&value&&typeof value==='object')schemas[name]=value;}
const text=JSON.stringify({upstream:'4a080252f06caf63a525d7873c5d94d3c23887e0',typebox:'1.3.27',schemas},null,2)+'\n';
const destination=process.env.MODEL_SCHEMA_OUTPUT??path.resolve('model_schemas_generated.json');
fs.writeFileSync(destination,text);
console.log(`Generated${Object.keys(schemas).length} official model/compat schemas at ${destination}`);
