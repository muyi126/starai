import assert from 'node:assert/strict';
import test from 'node:test';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import {MULTIMEDIA_TEMPLATES, clearTemplateConnection} from './multimedia-templates.ts';

test('legacy Dola edit, menu and restore preserve multipart 30s independently of TopEn JSON',()=>{
 const text=readFileSync(new URL('../app/admin/models/page.tsx',import.meta.url),'utf8');
 const file=ts.createSourceFile('models.tsx',text,ts.ScriptTarget.Latest,true,ts.ScriptKind.TSX);
 const needed=['LEGACY_DOLA_SEEDANCE_TEMPLATE_KEY','DOLA_SEEDANCE_TEMPLATE_KEY','openEdit','safeParseJson','setConnection','applyDolaSeedance30s','applyDolaSeedance2'];
 const declarations=new Map();let menu,restore;
 function visit(node){
  if(ts.isVariableDeclaration(node)&&needed.includes(node.name.getText(file)))declarations.set(node.name.getText(file),node.getText(file));
  if(ts.isJsxOpeningElement(node)){
   const attributes=node.attributes.properties;
   if(node.tagName.getText(file)==='select'&&attributes.some(a=>a.name?.text==='value'&&a.initializer?.getText(file)==='{videoTemplateKey}'))menu=attributes.find(a=>a.name?.text==='onChange').initializer.expression.getText(file);
   if(node.tagName.getText(file)==='button'){const a=attributes.find(a=>a.name?.text==='onClick');if(a?.initializer?.getText(file).includes('applyDolaSeedance30s'))restore=a.initializer.expression.getText(file);}
  }
  ts.forEachChild(node,visit);
 }
 visit(file);assert.equal(declarations.size,needed.length);assert.ok(menu);assert.ok(restore);
 let form,selected;
 const context={MULTIMEDIA_TEMPLATES,clearTemplateConnection,getSeedanceVariantByTemplateKey:()=>null,buildSeedancePriceRule:()=>({billing_type:'per_token'}),
 setForm:fn=>{form=typeof fn==='function'?fn(form):fn;},setVideoTemplateKey:key=>{selected=key;context.videoTemplateKey=key;},setAudioTemplateKey:()=>{},setErr:()=>{},setUpstreamModels:()=>{},setUpstreamModelsError:()=>{},setShowForm:()=>{}};
 const code=[...declarations.values()].map(s=>'const '+s+';').join('\n')+'\nglobalThis.open=openEdit;globalThis.menu='+menu+';globalThis.restore='+restore+';';
 vm.runInNewContext(ts.transpileModule(code,{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText,context);
 for(const [adapter,key,endpoint,duration] of [['dola_seedance_30s','dola_seedance_30s','/api/v1/videos',30],['topenrouter_seedance_2','dola_topenrouter_seedance_2','/v1/video/tasks',4]]){
  context.open({id:1,code:'custom',display_name:'Custom',category:'video',new_api_model:'custom',new_api_endpoint:endpoint,request_mode:'video',runtime_rule:{upstream:{adapter}},new_api_extra_params:{connection:{api_key:'test-key'}}});
  assert.equal(selected,key);assert.equal(JSON.parse(form.runtime_rule).upstream.adapter,adapter,'opening does not replace adapter');
  context.restore();assert.equal(form.new_api_endpoint,endpoint);assert.equal(JSON.parse(form.runtime_rule).upstream.adapter,adapter);assert.equal(JSON.parse(form.default_params).duration,duration);assert.equal(form.code,'custom');assert.equal(JSON.parse(form.new_api_extra_params).connection.api_key,'test-key');
  context.menu({target:{value:key}});assert.equal(form.new_api_endpoint,endpoint);assert.equal(JSON.parse(form.runtime_rule).upstream.adapter,adapter);
 }
});
