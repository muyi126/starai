import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
import { MULTIMEDIA_TEMPLATES, applyMultimediaTemplate, configureZexUploadProfile } from "../../../../admin/src/lib/multimedia-templates.ts";
import { buildVideoTaskParams, selectVideoTaskMedia, parseVideoRuntime, videoReferenceCapacity } from "../../../../../packages/shared-types/src/videoModel.ts";

function sourceFile(path) {
  return ts.createSourceFile(path, readFileSync(new URL(path, import.meta.url), "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
}
function findNode(source, predicate) {
  let found;
  function visit(node) { if (!found && predicate(node)) found = node; if (!found) ts.forEachChild(node, visit); }
  visit(source);
  assert.ok(found);
  return found;
}
function execute(text, context) {
  vm.runInNewContext(ts.transpileModule(text, { compilerOptions: { target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.React, module: ts.ModuleKind.CommonJS } }).outputText, context);
}
function elements(node) {
  if (Array.isArray(node)) return node.flatMap(elements);
  if (!node || typeof node !== "object") return [];
  return [node, ...elements(node.props?.children)];
}
const React = { Fragment: "fragment", createElement: (type, props, ...children) => ({ type, props: { ...props, children } }) };

test("admin Zex reference maximum updates the effective image slot without changing native frame limits", () => {
  const source = sourceFile("../../../../admin/src/app/admin/models/page.tsx");
  const context = { safeParseJson: (text, fallback) => { try { return JSON.parse(text); } catch { return fallback; } } };
  for (const name of ["getVideoRule", "setVideoRule"]) {
    const declaration = findNode(source, n => ts.isVariableDeclaration(n) && n.name.getText(source) === name);
    execute(`var ${declaration.getText(source)};`, context);
  }
  const runtime = adapter => JSON.stringify({ upstream: { adapter }, video: { max_reference_images: 9, max_reference_total: 9, reference_images: { key: "custom_images", max: 1 }, frames: { first: { max: 1 }, last: { max: 1 } } } });
  const corrected = JSON.parse(context.setVideoRule(runtime("zex_video"), { max_reference_images: 9 }));
  assert.equal(corrected.video.reference_images.max, 9);
  assert.equal(corrected.video.reference_images.key, "custom_images");
  assert.equal(corrected.video.max_reference_total, 9);
  assert.equal(corrected.video.frames.first.max, 1);
  assert.equal(corrected.video.frames.last.max, 1);
  const limited = JSON.parse(context.setVideoRule(JSON.stringify(corrected), { max_reference_images: 4 }));
  assert.equal(limited.video.reference_images.max, 4);
  assert.equal(JSON.parse(context.setVideoRule(runtime("zex_video"), { ref_slot_max: 3 })).video.max_reference_images, 3);
  assert.equal(JSON.parse(context.setVideoRule(runtime("native_media"), { max_reference_images: 9 })).video.reference_images.max, 1);
});

test("Zex toolbar keeps the asset library and role selector in every mode and routes picks to the selected material slots", () => {
  const source = sourceFile("ModelWorkspace.tsx");
  const branch = findNode(source, n => ts.isConditionalExpression(n) && n.condition.getText(source) === "(isGatewayVideo || isAliyunMultimodal || isAliyunHappyHorse)");
  const item = url => ({ url, name: url, public_id: url });
  for (const [mode, wanted] of [["text", ["all"]], ["first_frame", ["first_frame"]], ["first_last", ["first_frame", "last_frame"]], ["image", ["reference_images"]], ["audio", ["reference_audios"]], ["video_audio", ["reference_videos", "reference_audios"]], ["image_video_audio", ["reference_images", "reference_videos", "reference_audios"]]]) {
    let media = { first_frame: item("first"), last_frame: item("last"), reference_images: [item("image")], reference_videos: [], reference_audios: [] };
    const context = { React, ChatTopTools: "tools", isGatewayVideo: true, videoMaterialMode: mode, bottom: {}, setBottom() {}, videoMedia: media, veoFirstFrameAssets: [media.first_frame], veoLastFrameAssets: [media.last_frame], videoConfig: { reference_images: { max: 4 }, reference_videos: { max: 2 }, reference_audios: { max: 1 } }, maxVideoAssetRefs: 4, t: k => k, setVideoMedia: update => { media = update(media); } };
    context.videoReferenceCapacity = videoReferenceCapacity;
    context.videoReferenceBudget = videoReferenceCapacity(context.videoConfig, media, mode);
    execute(`var tree = ${branch.whenTrue.getText(source)};`, context);
    const tools = elements(context.tree).filter(n => n.type === "tools");
    assert.equal(tools.filter(n => n.props.showRole !== false).length, 1, `${mode} must show the role selector exactly once`);
    const libraries = tools.filter(n => n.props.showAssets !== false);
    assert.equal(libraries.length, wanted.length, `${mode} must show the corresponding asset libraries`);
    libraries.forEach((library, index) => {
      const slot = wanted[index];
      if (slot === "all") { assert.equal(library.props.referencePickMode, undefined); return; }
      assert.equal(library.props.referencePickMode, true);
      assert.equal(library.props.referenceAssetKind || "image", slot === "reference_videos" ? "video" : slot === "reference_audios" ? "audio" : "image");
      library.props.onReferenceImagesChange([item(`picked-${slot}`)]);
      assert.equal((Array.isArray(media[slot]) ? media[slot][0] : media[slot]).url, `picked-${slot}`);
    });
  }
  // The current Seedance 2.5 template has no video/audio slots.
  const template = MULTIMEDIA_TEMPLATES.find(t => t.key === "zex_seedance_2_5");
  assert.ok(template);
  const runtime = parseVideoRuntime(JSON.parse(applyMultimediaTemplate({ new_api_model: "", new_api_extra_params: "{}", runtime_rule: "{}" }, template.key).runtime_rule));
  assert.equal(runtime.reference_videos.max, 0);
  assert.equal(runtime.reference_audios.max, 0);
});

test("video toolbar honors configured material labels and descriptions over generic translations", () => {
  const source = sourceFile("video/VideoOptionToolbar.tsx");
  const ctx = { FIELD_DESC_KEY: { generation_mode: "generic-mode" }, enumLabel: (prop,value)=>prop.enumLabels?.[value] || String(value) };
  for (const name of ["optionLabel", "fieldDesc"]) {
    const declaration = findNode(source, n=>ts.isFunctionDeclaration(n) && n.name?.text===name);
    execute(declaration.getText(source),ctx);
  }
  const t = key=>key.endsWith(".reference") ? "参考图" : "通用说明";
  assert.equal(ctx.optionLabel(t,s=>s,"generation_mode",{enumLabels:{reference:"自由参考组合"}},"reference"),"自由参考组合");
  assert.equal(ctx.fieldDesc(t,s=>s,"generation_mode",{description:"选择参考素材组合，上传对应素材。"}),"选择参考素材组合，上传对应素材。");
});

test("existing system asset dialog filters the requested media kind and confirms into the reference callback", () => {
  const source = sourceFile("BottomBar.tsx");
  const dialog = findNode(source, n => ts.isJsxSelfClosingElement(n) && n.tagName.getText(source) === "SystemAssetLibraryDialog");
  for (const kind of ["image", "video", "audio"]) {
    let picks;
    const context = { React, SystemAssetLibraryDialog: "dialog", assetOpen: true, referencePickMode: true, referenceAssetKind: kind, assetLibraryLabel: "library", t: k => k, td: (k, fallback) => fallback, referenceImages: [], selectedAssets: [], maxReferenceImages: 2, referenceAssetsOnly: false, setAssetOpen() {}, onReferenceImagesChange: items => { picks = items; } };
    execute(`var tree = ${dialog.getText(source)};`, context);
    assert.equal(context.tree.props.kind, kind);
    assert.equal(context.tree.props.allowInspiration, kind === "image");
    context.tree.props.onConfirm([{ url: "picked", name: "picked", public_id: "asset-id", metadata: { duration: 5 } }]);
    assert.equal(picks[0].public_id, "asset-id");
    assert.equal(picks[0].duration_seconds, kind === "image" ? undefined : 5);
  }
});

test("Seedance reference uploads and asset picks share one mixed-media budget", async () => {
  const item = name => ({ url: `https://e.test/${name}`, name });
  const items = (kind, count) => Array.from({ length: count }, (_, i) => item(`${kind}${i}`));
  const runtime = { upstream: { adapter: "zex_video" }, video: { upload_profile: "seedance_2", max_reference_total: 9, reference_images: { max: 9 }, reference_videos: { max: 9 }, reference_audios: { max: 9 } } };
  const config = parseVideoRuntime(runtime);
  assert.equal(config.max_reference_total, 9);
  assert.equal(parseVideoRuntime({ upstream: { adapter: "zex_video" } }).max_reference_total, 9);
  assert.equal(parseVideoRuntime({}).max_reference_total, undefined);
  const media = { first_frame: null, last_frame: null, reference_images: items("image", 4), reference_videos: [], reference_audios: items("audio", 3) };
  let budget = videoReferenceCapacity(config, media, "image_audio");
  assert.equal(budget.total, 7);
  assert.equal(budget.remaining, 2);
  assert.equal(budget.reference_images, 6);
  assert.equal(budget.reference_audios, 5);
  assert.equal(videoReferenceCapacity(config, media, "image").reference_images, 9, "hidden audio must not consume the active mode's budget");
  assert.equal(videoReferenceCapacity(config, { ...media, reference_audios: items("audio", 5) }, "image_audio").remaining, 0);
  assert.equal(videoReferenceCapacity(config, { ...media, reference_audios: items("audio", 4) }, "image_audio").remaining, 1);
  budget = videoReferenceCapacity(config, { ...media, reference_videos: items("video", 2) }, "image_video_audio");
  assert.equal(budget.total, 9);
  assert.equal(budget.remaining, 0);
  const uploadSource = sourceFile("video/VideoUploadArea.tsx");
  const alerts = [];
  let uploads = 0;
  const context = { exports: {}, React, useState: () => [false, () => {}], useI18n: () => ({ t: key => key, ts: key => key }), videoReferenceCapacity, ArrowRight: "arrow", Film: "film", Music2: "music", Plus: "plus", UserRound: "portrait", X: "close", SeedancePortraitDialog: "portrait-dialog", alert: message => alerts.push(message), uploadAsset: async file => { uploads++; return { url: file.name, name: file.name }; } };
  execute(uploadSource.statements.filter(n => !ts.isImportDeclaration(n)).map(n => n.getText(uploadSource)).join("\n"), context);
  const grokConfig = { ...config, upload_profile: "aliyun_multimodal", max_reference_total: 7, reference_images: { max: 7 }, reference_videos: { max: 0 }, reference_audios: { max: 0 } };
  const grokTree = context.VideoUploadArea({ config: grokConfig, media: { ...media, reference_audios: [] }, mode: "reference", showBudgetNotice: false, onChange() {} });
  assert.equal(elements(grokTree).some(n => n.props?.role === "status"), false, "Grok reference upload keeps its capacity without a duplicate quota notice");
  assert.equal(elements(grokTree).some(n => n.type === "arrow"), false, "image-only reference groups have no dangling media arrows");
  for (const mode of ["image", "video", "image_audio", "image_video", "video_audio", "image_video_audio"]) {
    const modeTree = context.VideoUploadArea({ config, media, mode, onChange() {} });
    assert.equal(elements(modeTree).some(n => n.props?.role === "status"), mode !== "image" && mode !== "video", `${mode} shows the shared quota notice only for mixed materials`);
  }
  for (const mode of ["image", "image_audio", "image_video", "image_video_audio"]) {
    const empty = { first_frame: null, last_frame: null, reference_images: [], reference_videos: [], reference_audios: [] };
    const modeTree = context.VideoUploadArea({ config, media: empty, mode, onChange() {} });
    const stack = elements(modeTree).find(n => n.type === context.ReferenceImageStack);
    assert.equal(stack.props.max, 9, `${mode} must allow multiple reference images`);
    const emptyStack = context.ReferenceImageStack(stack.props);
    assert.equal(emptyStack.props.multiple, true, `${mode} enables batch selection on the first upload`);
    const box = context.EmptyUploadBox(emptyStack.props);
    assert.equal(elements(box).find(n => n.type === "input").props.multiple, true);
  }
  const frame = context.FrameSlot({ label: "first", image: null, uploading: false, onUpload() {} });
  assert.equal(context.EmptyUploadBox(frame.props).props.children.find(n => n?.type === "input").props.multiple, undefined, "frame upload stays single-image");
  let updated;
  const tree = context.VideoUploadArea({ config, media, mode: "image_audio", onChange: next => { updated = next; } });
  const imageStack = elements(tree).find(n => n.type === context.ReferenceImageStack);
  assert.equal(imageStack.props.max, 6);
  await imageStack.props.onUpload([{ name: "a" }, { name: "b" }, { name: "c" }]);
  assert.equal(uploads, 0, "reject excess batch before uploading assets");
  assert.equal(alerts.length, 1);
  await imageStack.props.onUpload([{ name: "a" }, { name: "b" }]);
  assert.equal(updated.reference_images.length + updated.reference_audios.length, 9);
  const fullTree = context.VideoUploadArea({ config, media: updated, mode: "image_audio", onChange() {} });
  assert.equal(elements(fullTree).filter(n => n.type === context.EmptyUploadBox).length, 0, "no audio add slot when combined budget is full");
  assert.equal(elements(fullTree).find(n => n.type === context.ReferenceImageStack).props.max, 6);
  const toolbarSource = sourceFile("ModelWorkspace.tsx");
  const branch = findNode(toolbarSource, n => ts.isConditionalExpression(n) && n.condition.getText(toolbarSource) === "(isGatewayVideo || isAliyunMultimodal || isAliyunHappyHorse)");
  let current = media;
  const toolbar = { React, ChatTopTools: "tools", isGatewayVideo: true, videoMaterialMode: "image_audio", bottom: {}, setBottom() {}, videoMedia: media, videoConfig: config, videoReferenceBudget: videoReferenceCapacity(config, media, "image_audio"), videoReferenceCapacity, t: key => key, setVideoMedia: update => { current = update(current); } };
  execute(`var tree = ${branch.whenTrue.getText(toolbarSource)};`, toolbar);
  const libraries = elements(toolbar.tree).filter(n => n.type === "tools" && n.props.referencePickMode);
  assert.deepEqual(libraries.map(n => n.props.maxReferenceImages), [6, 5]);
  libraries[0].props.onReferenceImagesChange(items("chosen", 6));
  libraries[1].props.onReferenceImagesChange(items("audio", 5));
  assert.equal(current.reference_images.length + current.reference_audios.length, 9, "confirming an older asset picker rechecks the latest shared budget");
  const assetSource = sourceFile("SystemAssetLibraryDialog.tsx");
  const toggle = findNode(assetSource, n => ts.isVariableDeclaration(n) && n.name.getText(assetSource) === "toggle");
  let selected = [];
  const picker = { disabled: new Set(), draft: [], maxSelected: 0, keyOf: item => item.url, setDraft: update => { selected = typeof update === "function" ? update(selected) : update; } };
  execute(`var ${toggle.getText(assetSource)};`, picker);
  picker.toggle(item("blocked"));
  assert.equal(selected.length, 0, "a full shared budget must not allow an extra asset through the single-select branch");
});

test("existing upload components and submitted materials follow the selected Zex shape and mode", () => {
  const source = sourceFile("video/VideoUploadArea.tsx");
  const ctx = {exports:{},React,useState:()=>[false,()=>{}],useI18n:()=>({t:k=>k,ts:k=>k}),videoReferenceCapacity,ArrowRight:"arrow",Film:"film",Music2:"music",Plus:"plus",UserRound:"portrait",X:"close",SeedancePortraitDialog:"portrait-dialog"};
  execute(source.statements.filter(n=>!ts.isImportDeclaration(n)).map(n=>n.getText(source)).join("\n"),ctx);
  const template=MULTIMEDIA_TEMPLATES.find(t=>t.key==="zex_seedance_2_0");
  const form=applyMultimediaTemplate({new_api_model:"",new_api_extra_params:"{}",runtime_rule:"{}"},template.key);
  const item=url=>({url,name:url,public_id:url});
  const media={first_frame:item("first"),last_frame:item("last"),reference_images:[item("ref")],reference_videos:[item("video")],reference_audios:[item("audio")]};
  for(const [profile,mode,wantFrames,wantKinds] of [["aliyun_multimodal","first_last",2,[]],["seedance_2","first_last",2,[]],["seedance_2","image_audio",0,["reference_images","reference_audios"]],["multi_ref","first_frame",1,[]],["multi_ref","first_last",2,[]],["single_ref","first_last",2,[]],["multi_ref","reference",0,["reference_images"]],["multi_ref","text",0,[]]]) {
    const configured=configureZexUploadProfile(form,profile);
    const runtime=JSON.parse(configured.runtime_rule);
    const tree=ctx.VideoUploadArea({config:parseVideoRuntime(runtime),media,onChange:()=>{},mode});
    const nodes=elements(tree);
    assert.equal(nodes.filter(n=>n.type===ctx.FrameSlot).length,wantFrames);
    assert.equal(nodes.some(n=>n.type==="portrait"),false);
    if(mode==="text") assert.equal(tree,null);
    const payload=buildVideoTaskParams({generation_mode:mode,reference_images:["stale"],first_frame:"stale",last_frame:"stale"},media,runtime);
    const active=selectVideoTaskMedia({generation_mode:mode},media,runtime);
    assert.equal(active.first_frame?.public_id,wantFrames ? "first":undefined);
    assert.equal(active.last_frame?.public_id,wantFrames===2 ? "last":undefined);
    for(const key of ["reference_images","reference_videos","reference_audios"]) assert.equal(active[key].length,wantKinds.includes(key) ? 1 : 0);
    for(const key of ["reference_images","reference_videos","reference_audios"]) assert.equal(key in payload,wantKinds.includes(key));
    assert.equal(payload.first_frame,wantFrames ? "first":undefined);
    assert.equal(payload.last_frame,wantFrames===2 ? "last":undefined);
    if(wantKinds.includes("reference_images")) assert.deepEqual(payload.reference_images,["ref"]);
  }
  const agent=sourceFile("AgentWorkspace.tsx");
  const modeDeclaration=findNode(agent,n=>ts.isVariableDeclaration(n) && n.name.getText(agent)==="videoMaterialMode");
  for(const [adapter,profile,wantMode] of [["zex_video","multi_ref","first_last"],["native_media","multi_ref",undefined],["volcengine_seedance_2","seedance_2","first_last"]]) {
    const modeContext={generationModel:{runtime_rule:{upstream:{adapter}}},videoConfig:{upload_profile:profile},params:{generation_mode:"first_last"}};
    execute(`var ${modeDeclaration.getText(agent)};`,modeContext);
    assert.equal(modeContext.videoMaterialMode,wantMode);
  }
  assert.ok(ctx.VideoUploadArea({config:{upload_profile:"multi_ref",max_reference_images:4},media,onChange:()=>{}}));
});

test("MiniMax material combinations hide inactive stale uploads and submit only selected kinds", () => {
  const source = sourceFile("video/VideoUploadArea.tsx");
  const ctx = {exports:{},React,useState:()=>[false,()=>{}],useI18n:()=>({t:k=>k,ts:k=>k}),videoReferenceCapacity,ArrowRight:"arrow",Film:"film",Music2:"music",Plus:"plus",UserRound:"portrait",X:"close",SeedancePortraitDialog:"portrait-dialog"};
  execute(source.statements.filter(n=>!ts.isImportDeclaration(n)).map(n=>n.getText(source)).join("\n"),ctx);
  for (const key of ["zex_minimax_h3", "zex_minimax_h3_max"]) {
    const runtime = MULTIMEDIA_TEMPLATES.find(t=>t.key===key).runtime;
    const item = url=>({url,name:url});
    const media = {first_frame:item("first"),last_frame:item("last"),reference_images:[item("image")],reference_videos:[item("video")],reference_audios:[item("audio")]};
    for (const mode of ["text","image","video","audio","image_audio","image_video","video_audio","image_video_audio","reference"]) {
      const nodes = elements(ctx.VideoUploadArea({config:parseVideoRuntime(runtime),media,onChange(){},mode}));
      const payload = buildVideoTaskParams({generation_mode:mode},media,runtime);
      assert.equal(nodes.some(n=>n.type===ctx.FrameSlot),false);
      for (const [kind,slot] of [["image","reference_images"],["video","reference_videos"],["audio","reference_audios"]]) {
        const active = mode==="reference" || mode.split("_").includes(kind);
        const visible = kind==="image" ? nodes.some(n=>n.type===ctx.ReferenceImageStack) : nodes.some(n=>n.type===ctx.FilledFileCard && n.props.kind===kind);
        assert.equal(visible,active,`${key}/${mode}/${kind}`);
        assert.equal(slot in payload,active);
      }
      assert.equal(payload.first_frame,undefined);
      assert.equal(payload.last_frame,undefined);
      assert.equal(videoReferenceCapacity(parseVideoRuntime(runtime),media,mode).total, mode==="reference" ? 3 : mode==="text" ? 0 : mode.split("_").length);
    }
  }
});

test("real numeric schema controls allow decimals, omit cleared optional seeds and retain integer errors for validation", () => {
  const source = sourceFile("SchemaForm.tsx");
  const context = { exports: {}, React, useI18n: () => ({ ts: (value) => value }) };
  execute(source.statements.filter((node) => !ts.isImportDeclaration(node)).map((node) => node.getText(source)).join("\n"), context);
  for (const [type, step] of [["number", "any"], ["integer", 1]]) {
    const changes = [];
    const tree = context.SchemaForm({ schema: { properties: { seed: { type, title: "种子" } } }, values: {}, onChange: (next) => changes.push(next) });
    const input = elements(tree).find((node) => node.type === "input");
    assert.equal(input.props.step, step);
    assert.ok(input.props.className.includes("dark:"));
    input.props.onChange({ target: { value: "" } });
    assert.equal(changes[0].seed, undefined);
    assert.equal(JSON.stringify(changes[0]), "{}");
    input.props.onChange({ target: { value: "2.5" } });
    assert.equal(changes[1].seed, 2.5);
  }
});

test("actual image count menu supports MiniMax 1-9 and obeys custom-count restrictions", () => {
  const source = sourceFile("ImageGenerationToolbar.tsx");
  const changes = [];
  const context = { exports: {}, React, useMemo: (fn) => fn(), useState: () => ["9", () => {}], useI18n: () => ({ t: (key) => key, ts: (key) => key }), Grid3X3: "icon", FileText: "icon", Settings: "icon", MediaOptionMenu: "menu", MediaMenuOption: "option" };
  execute(source.statements.filter((node) => !ts.isImportDeclaration(node)).map((node) => node.getText(source)).join("\n"), context);
  const props = { count: 1, onCountChange: (n) => changes.push(n), ratio: "1:1", onRatioChange: () => {}, imageSize: "1K", onImageSizeChange: () => {}, countOptions: [1, 2, 3, 4], countMax: 9, showSizeTier: false };
  const countMenu = elements(context.ImageGenerationToolbar(props)).find((node) => node.type === "menu");
  const menu = elements(countMenu.props.children[0](() => {}));
  assert.equal(menu.find((node) => node.type === "input").props.max, 9);
  menu.find((node) => node.type === "button").props.onClick();
  assert.deepEqual(changes, [9]);
  assert.equal(context.buildImageGenerationParams({ count: 9, ratio: "1:1" }).n, 9);
  const restricted = elements(context.ImageGenerationToolbar({ ...props, countAllowCustom: false })).find((node) => node.type === "menu");
  assert.equal(elements(restricted.props.children[0](() => {})).some((node) => node.type === "input"), false);
});

test("model-switch effect resets inherited image count to the new model default and maximum", () => {
  const source = sourceFile("ModelWorkspace.tsx");
  const block = findNode(source, (node) => ts.isIfStatement(node) && node.expression.getText(source) === "isImage" && node.thenStatement.getText(source).includes("setImageCount"));
  for (const [defaults, maximum, expected] of [[{}, 1, 1], [{ count: 9 }, 9, 9], [{ count: 9 }, 1, 1], [{ n: 3 }, 9, 3]]) {
    let count = 9;
    execute(block.getText(source), { isImage: true, defaults, model: { runtime_rule: {} }, imageCountMax: maximum, imageAllowsAutoRatio: false, setImageCount: (value) => { count = value; }, setImageSize: () => {}, setImageRatio: () => {}, defaultImageSizeForConfig: () => "1K", normalizeRatio: (value) => value });
    assert.equal(count, expected);
  }
});

test("submit guards distinguish required first-frame video, optional first-frame and reference images", async () => {
  const source = sourceFile("ModelWorkspace.tsx");
  const handler = findNode(source, (node) => ts.isVariableDeclaration(node) && node.name.getText(source) === "handleMediaTask");
  const statements = handler.initializer.body.statements;
  const stop = statements.findIndex((node) => ts.isVariableStatement(node) && node.declarationList.declarations[0].name.getText(source) === "requestVersion");
  assert.ok(stop > 0);
  const prefix = statements.slice(0, stop).map((node) => node.getText(source)).join("\n");
  for (const [overrides, expected] of [
    [{ videoConfig: { upload_profile: "first_frame", min_reference_images: 1 }, workbenchInputSchema: { required: ["first_frame"] }, videoMedia: { first_frame: { url: "test-image" }, reference_images: [] } }, true],
    [{ videoConfig: { upload_profile: "first_frame", min_reference_images: 1 }, workbenchInputSchema: { required: ["first_frame"] } }, false],
    [{ videoConfig: { upload_profile: "first_frame", min_reference_images: 0 } }, true],
    [{ videoConfig: { upload_profile: "multi_ref", min_reference_images: 1 } }, false],
    [{ videoConfig: { upload_profile: "multi_ref", min_reference_images: 1 }, videoMedia: { reference_images: [{ url: "test-image" }] } }, true],
    [{ prompt: "", videoConfig: { upload_profile: "multi_ref", min_reference_images: 1, prompt_required: false }, videoMedia: { reference_images: [{ url: "test-image" }] } }, true],
    [{ isGatewayVideo: true, videoMaterialMode: "first_frame", videoMedia: { reference_images: [], reference_videos: [], reference_audios: [] } }, false],
    [{ isGatewayVideo: true, videoMaterialMode: "first_frame", videoMedia: { first_frame: { url: "first.png" }, reference_images: [], reference_videos: [], reference_audios: [] } }, true],
    [{ isGatewayVideo: true, videoMaterialMode: "reference", videoMedia: { reference_images: [], reference_videos: [], reference_audios: [{ url: "ref.mp3" }] } }, true],
    [{ videoReferenceBudget: { total: 10, limit: 9 } }, false],
    [{ isVideo: false, isImage: true, imageRuntime: { min_reference_images: 1 }, refImages: [] }, false],
    [{ isVideo: false, isAudio: true, workbenchInputSchema: { required: ["voice"], properties: { voice: { type: "string", title: "音色 ID" } } }, params: { voice: " " } }, false],
    [{ isVideo: false, isAudio: true, workbenchInputSchema: { required: ["voice"], properties: { voice: { type: "string", title: "音色 ID" } } }, params: { voice: "voice-id" } }, true],
  ]) {
    const alerts = [];
    const context = { isVideo: true, isAudio: false, isImage: false, prompt: "test prompt", videoConfig: {}, audioConfig: {}, imageRuntime: {}, workbenchInputSchema: {}, params: {}, schemaProperties: (schema) => schema.properties || {}, videoMedia: { reference_images: [] }, refImages: [], isMiniMaxH3: false, isGatewayVideo: false, videoMaterialMode: "text", isAliyunMultimodal: false, isAliyunHappyHorse: false, isVeoFramePair: false, isVeoReference: false, isOmniReference: false, t: (key) => key, ts: (key) => key, alert: (message) => alerts.push(message), ...overrides };
    context.videoReferenceBudget ||= { total: 0, limit: undefined };
    execute(`async function validate() { ${prefix}\nreturn true; }`, context);
    assert.equal(await context.validate(), expected ? true : undefined);
    assert.equal(alerts.length, expected ? 0 : 1);
  }
});

test("custom audio/video scalar fields use the existing accessible themed SchemaForm", () => {
  for (const path of ["audio/AudioOptionToolbar.tsx", "video/VideoOptionToolbar.tsx"]) {
    const source = ts.createSourceFile(path, readFileSync(new URL(path, import.meta.url), "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    const render = source.statements.find((node) => ts.isFunctionDeclaration(node) && node.name?.text === "renderFieldControl");
    const SchemaForm = () => null;
    const context = { SchemaForm, React: { createElement: (type, props) => ({ type, props }) } };
    vm.runInNewContext(ts.transpileModule(render.getText(source), { compilerOptions: { target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.React } }).outputText, context);
    for (const prop of [{ type: "number", title: "语速", minimum: 0.25 }, { type: "string", title: "音色 ID" }]) {
      const changes = [];
      const element = context.renderFieldControl("custom", prop, undefined, (key, value) => changes.push([key, value]));
      assert.equal(element.type, SchemaForm);
      assert.equal(element.props.schema.properties.custom, prop);
      element.props.onChange({ custom: prop.type === "number" ? 1.2 : "voice-id" });
      assert.deepEqual(changes, [["custom", prop.type === "number" ? 1.2 : "voice-id"]]);
    }
  }
});
