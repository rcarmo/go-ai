// Generate shared model/compat schemas from official pi-ai 1.5.0 and its
// pinned pi-schema 1.5.0 dependency. Install the dependency alongside the
// source beneath the resolved project root; no global npm install.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
const source=process.env.PI_AI_SCHEMA_SOURCE;
if(!source||!path.isAbsolute(source))throw Error('PI_AI_SCHEMA_SOURCE must name the absolute pinned source root');
const pkg=JSON.parse(fs.readFileSync(path.join(source,'packages/ai/package.json'),'utf8'));
if(pkg.version!=='1.5.0'||pkg.dependencies['@earendil-works/pi-schema']!=='1.5.0')throw Error('incorrect schema package/dependency pin');
const dependency=JSON.parse(fs.readFileSync(path.join(source,'node_modules/@earendil-works/pi-schema/package.json'),'utf8'));
if(dependency.name!=='@earendil-works/pi-schema'||dependency.version!=='1.5.0')throw Error('incorrect installed pi-schema pin');
const pinned={
 'model-schema.ts':'446f06d10c8a60be5749bfad79fb36c70401c2d746448fa915ade9be82b827b0',
 'compat-schema.ts':'49707bc06bcb12ef6f908b95f9cd17b7eaff1765b126942503d7f09c2870bdaf',
 'schema-builders.ts':'5309cc3d78663f0234cffea2cf5ca9765f2d69c6bace52991ea8c9e42b6e6e0a'
};
for(const [file,expected] of Object.entries(pinned)){
 const actual=crypto.createHash('sha256').update(fs.readFileSync(path.join(source,'packages/ai/src/providers',file))).digest('hex');
 if(actual!==expected)throw Error(`schema source differs from exact official1.5.0: ${file}`);
}
const model=await import(path.join(source,'packages/ai/src/providers/model-schema.ts'));
const compat=await import(path.join(source,'packages/ai/src/providers/compat-schema.ts'));
const schemas:Record<string,unknown>={};
for(const module of [model,compat])for(const [name,value] of Object.entries(module)){if(name.endsWith('Schema')&&value&&typeof value==='object')schemas[name]=value;}
const text=JSON.stringify({upstream:'f9a26365568127461c47a07a37dcb42c140a3ac5',piSchema:'1.5.0',schemas},null,2)+'\n';
const destination=process.env.MODEL_SCHEMA_OUTPUT??path.resolve('model_schemas_generated.json');
fs.writeFileSync(destination,text);
console.log(`Generated${Object.keys(schemas).length} official model/compat schemas at ${destination}`);
