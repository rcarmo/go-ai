#!/usr/bin/env python3
"""Offline schema-v6 hydration conformance/fault tests (no live model fetch)."""
import hashlib,json,os,pathlib,shutil,subprocess,tempfile
repo=pathlib.Path(__file__).resolve().parents[1]
source=pathlib.Path(os.environ.get('PI_AI_MODELS_GENERATED_JS','/workspace/tmp/pi-ai-audit-101/new-package/package/dist/models.generated.js')).parent/'providers/data'
with tempfile.TemporaryDirectory(prefix='go-ai-v101-hydrate-') as tmp:
 root=pathlib.Path(tmp);binary=root/'generator'
 subprocess.run(['go','build','-o',str(binary),'./scripts/generate-models.go'],cwd=repo,check=True)
 for kind,count in [('chat',1536),('image',59),('classifier',20)]:
  a=root/(kind+'.go');b=root/(kind+'-again.go')
  for dest in [a,b]:
   r=subprocess.run([str(binary),'-data-dir',str(source),'-kind',kind,'-output',str(dest)],capture_output=True,text=True)
   assert r.returncode==0,(r.stdout,r.stderr)
   assert 'Found '+str(count)+' '+kind in r.stderr,r.stderr
  assert a.read_bytes()==b.read_bytes(),'non-reproducible generator'
 for mode in ['missing-file','bad-manifest','bad-hash','identity','invalid-cost','duplicate-model','unexpected-file','input-modality','missing-image-output','invalid-image-output','forbidden-chat-output','forbidden-classifier-output']:
  directory=root/mode;shutil.copytree(source,directory);out=root/(mode+'.go');out.write_text('sentinel output\n')
  manifest_path=directory/'.manifest.json';manifest=json.loads(manifest_path.read_text())
  if mode=='missing-file':(directory/'anthropic.json').unlink()
  elif mode=='bad-manifest':manifest['schemaVersion']=5;manifest_path.write_text(json.dumps(manifest))
  elif mode=='bad-hash':(directory/'anthropic.json').write_text('{}')
  elif mode=='unexpected-file':(directory/'rogue.json').write_text('{}')
  else:
   filename='typesafe.json'
   if 'image-output' in mode: filename='openrouter.json'
   if mode=='forbidden-chat-output': filename='anthropic.json'
   file=directory/filename;data=json.loads(file.read_text())
   choices=[(g,k) for g,models in data.items() for k in models if k.startswith('image:' if 'image-output' in mode else 'chat:' if mode=='forbidden-chat-output' else 'classifier:')]
   group,key=choices[0]
   if mode=='identity':data[group][key]['id']='wrong'
   elif mode=='invalid-cost':data[group][key]['cost']['input']=None
   elif mode=='input-modality':data[group][key]['input']=['bogus']
   elif mode=='missing-image-output':data[group][key].pop('output',None)
   elif mode=='invalid-image-output':data[group][key]['output']=['text']
   elif mode.startswith('forbidden-'):data[group][key]['output']=['image']
   else:data['duplicate-api']={key:data[group][key]}
   file.write_text(json.dumps(data));manifest['files'][file.name]=hashlib.sha256(file.read_bytes()).hexdigest();manifest_path.write_text(json.dumps(manifest))
  r=subprocess.run([str(binary),'-data-dir',str(directory),'-kind','chat','-output',str(out)],capture_output=True,text=True)
  assert r.returncode!=0,mode+' did not fail'
  assert out.read_text()=='sentinel output\n',mode+' partially overwrote output'
  assert not list(out.parent.glob('.model-catalog-*')),'temporary output leaked'
print('offline schema-v6 hydration valid/reproducible and twelve fail-closed atomic fault cases passed')
