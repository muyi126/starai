import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";

const read = file => readFileSync(new URL(file, import.meta.url), "utf8");
const workspace = ts.createSourceFile("creative.tsx", read("./workbench/CreativeAgentWorkspace.tsx"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
const errorSources = workspace.statements.filter(ts.isVariableStatement).flatMap(statement => statement.declarationList.declarations).find(declaration => declaration.name.getText(workspace) === "CREATIVE_ERROR_SOURCES");
assert.ok(errorSources, "missing shared local error sources");
const CREATIVE_ERROR_SOURCES = vm.runInNewContext(`(${errorSources.initializer.getText(workspace)})`);

function effectCallFor(text, sourceFile = workspace) {
  let result;
  function visit(node) {
    if (ts.isCallExpression(node) && node.expression.getText(sourceFile) === "useEffect" && node.arguments[0]?.getText(sourceFile).includes(text)) {
      result = node;
    }
    ts.forEachChild(node, visit);
  }
  visit(sourceFile);
  assert.ok(result, `missing effect for ${text}`);
  return result;
}

function effectFor(path) {
  return effectCallFor(`"${path}"`).arguments[0].getText(workspace);
}

function renderEffect(call, context, sourceFile = workspace) {
  let previousDependencies;
  let cleanup;
  context.useEffect = (callback, dependencies) => {
    if (previousDependencies && dependencies.length === previousDependencies.length && dependencies.every((value, index) => Object.is(value, previousDependencies[index]))) return;
    cleanup?.();
    previousDependencies = dependencies;
    cleanup = callback();
  };
  const source = ts.transpileModule(call.getText(sourceFile), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
  const render = () => vm.runInNewContext(source, context);
  return { render, stop: () => cleanup?.() };
}

function executeEffect(source, apiCached, changes) {
  const context = { apiCached, CREATIVE_ERROR_SOURCES, t: value => value, isMusicModel: () => false, activeAgentChatModel: () => "chat" };
  for (const [, setter] of source.matchAll(/\b(set[A-Z]\w*)\(/g)) {
    context[setter] = value => { changes[setter] = value; };
  }
  vm.runInNewContext(ts.transpileModule(`globalThis.effect = ${source}`, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  return context.effect();
}

test("slow optional public configuration does not block creative model selection", async () => {
  const changes = {};
  let releaseConfig;
  const cached = path => path === "/api/system-configs/public"
    ? new Promise(resolve => { releaseConfig = resolve; })
    : Promise.resolve(path === "/api/models" ? [{ code: "image", category: "image" }, { code: "video", category: "video" }] : { runtime_config: {} });
  const stopModels = executeEffect(effectFor("/api/models"), cached, changes);
  const stopConfig = executeEffect(effectFor("/api/system-configs/public"), cached, changes);
  await new Promise(setImmediate);
  assert.equal(changes.setImageModelCode, "image");
  assert.equal(changes.setVideoModelCode, "video");
  assert.equal(changes.setSearchAvailable, undefined, "configuration should still be pending");
  releaseConfig({ web_search_enabled: true, web_search_unit_price: 0.1 });
  await new Promise(setImmediate);
  assert.equal(changes.setSearchAvailable, true);
  assert.equal(changes.setSearchUnitPrice, 0.1);
  stopModels();
  stopConfig();
});

test("workspace initialization ignores metadata responses after unmount", async () => {
  const changes = {};
  const releases = new Map();
  const cached = path => new Promise(resolve => { releases.set(path, resolve); });
  const stopModels = executeEffect(effectFor("/api/models"), cached, changes);
  const stopConfig = executeEffect(effectFor("/api/system-configs/public"), cached, changes);
  stopModels();
  stopConfig();
  releases.get("/api/models")([]);
  releases.get("/api/agents/general_creative_agent")({ runtime_config: {} });
  releases.get("/api/system-configs/public")({ web_search_enabled: true });
  await new Promise(setImmediate);
  assert.deepEqual(changes, {});
});

test("late translations preserve user-selected creative models without reloading metadata", async () => {
  const changes = {};
  const requests = [];
  const call = effectCallFor('"/api/models"');
  const context = {
    CREATIVE_ERROR_SOURCES,
    t: value => `initial:${value}`,
    isMusicModel: model => model.code.startsWith("music"),
    activeAgentChatModel: () => "chat-default",
    apiCached: path => {
      requests.push(path);
      return Promise.resolve(path === "/api/models" ? [
        { code: "image-default", category: "image" },
        { code: "video-default", category: "video" },
        { code: "speech-default", category: "audio" },
        { code: "music-default", category: "audio" },
      ] : { runtime_config: {} });
    },
  };
  for (const [, setter] of call.arguments[0].getText(workspace).matchAll(/\b(set[A-Z]\w*)\(/g)) {
    context[setter] = value => { changes[setter] = value; };
  }
  const effect = renderEffect(call, context);
  effect.render();
  await new Promise(setImmediate);
  for (const category of ["Chat", "Image", "Video", "Speech", "Music"]) changes[`set${category}ModelCode`] = `${category}-user-choice`;
  context.t = value => `override:${value}`;
  effect.render();
  await new Promise(setImmediate);
  context.t = value => `different-locale:${value}`;
  effect.render();
  await new Promise(setImmediate);
  for (const category of ["Chat", "Image", "Video", "Speech", "Music"]) assert.equal(changes[`set${category}ModelCode`], `${category}-user-choice`);
  assert.equal(requests.length, 2, "translations must not reload model metadata");
  effect.stop();
});

test("late translations do not cancel or replan a pending creative draft", () => {
  const pending = { planState: "pending" };
  let messages = [pending];
  let replans = 0;
  const context = {
    CREATIVE_ERROR_SOURCES,
    restoringSelection: { current: false }, selectionEpoch: { current: 0 },
    draftRef: { current: null }, conversationIdRef: { current: "conversation" }, sessionEpoch: { current: 1 },
    customEnabled: true, customMediaType: "image", imageModelCode: "chosen-image", videoModelCode: "chosen-video",
    speechModelCode: "chosen-speech", musicModelCode: "chosen-music", bottom: { files: [], asset_ids: [] },
    t: value => `initial:${value}`, setMessages: update => { messages = update(messages); },
    refreshPlan: () => { replans++; return Promise.resolve(); },
    window: { setTimeout: () => { replans++; return 1; }, clearTimeout: () => {} },
  };
  const effect = renderEffect(effectCallFor("selectionEpoch.current++"), context);
  effect.render();
  messages = [pending];
  context.draftRef.current = { status: "awaiting_confirmation" };
  context.t = value => `override:${value}`;
  effect.render();
  assert.equal(messages[0].planState, "pending");
  assert.equal(context.draftRef.current.status, "awaiting_confirmation");
  assert.equal(context.selectionEpoch.current, 1);
  assert.equal(replans, 0);
  context.t = value => `different-locale:${value}`;
  effect.render();
  assert.equal(messages[0].planState, "pending");
  assert.equal(replans, 0);
  context.imageModelCode = "new-user-selection";
  effect.render();
  assert.equal(messages[0].planState, "cancelled", "a real model change must invalidate the old draft");
  assert.equal(context.draftRef.current.status, "refreshing");
  assert.equal(replans, 1, "a real model change must schedule replanning");
  effect.stop();
});

test("metadata errors retain their source text and render with the latest translation", async () => {
  const changes = {};
  executeEffect(effectFor("/api/models"), () => Promise.reject(new Error("offline")), changes);
  await new Promise(setImmediate);
  assert.equal(changes.setError, "模型列表加载失败，请稍后重试");
  let errorExpression;
  function visit(node) {
    if (ts.isVariableDeclaration(node) && node.name.getText(workspace) === "displayedError") errorExpression = node.initializer.getText(workspace);
    ts.forEachChild(node, visit);
  }
  visit(workspace);
  assert.ok(errorExpression, "error must be translated at render time");
  const context = { CREATIVE_ERROR_SOURCES, error: changes.setError, t: value => `initial:${value}` };
  assert.equal(vm.runInNewContext(errorExpression, context), "initial:模型列表加载失败，请稍后重试");
  context.t = value => `override:${value}`;
  assert.equal(vm.runInNewContext(errorExpression, context), "override:模型列表加载失败，请稍后重试");
  context.error = "API custom error {count}: 已上传 2 个文件";
  assert.equal(vm.runInNewContext(errorExpression, context), context.error, "API and already formatted errors must remain unchanged");
});

function agentEffects(cached) {
  const file = ts.createSourceFile("agent.tsx", read("./workbench/AgentWorkspace.tsx"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const calls = [effectCallFor('setSelectedScene(code === "ecommerce_image"', file), effectCallFor("apiForLocaleCached<Workflow>", file)];
  const changes = { setBottom: { channel_key: "initial" } };
  const context = {
    code: "general_image", locale: "zh-CN", apiForLocaleCached: cached,
    workflowDefaultsCodeRef: { current: "" }, modelDefaultsCodeRef: { current: "" },
    EMPTY_VIDEO_MEDIA: {}, normalizeCreativeMode: value => value, normalizeRatio: value => value, normalizeTier: value => value,
    schemaDefaultsFromFields: schema => schema?.defaults || {},
  };
  for (const call of calls) {
    for (const [, setter] of call.arguments[0].getText(file).matchAll(/\b(set[A-Z]\w*)\(/g)) context[setter] = value => {
      changes[setter] = typeof value === "function" ? value(changes[setter]) : value;
    };
  }
  // Each effect has its own hook slot, as it does in the component.
  const effects = calls.map(call => {
    const local = Object.create(context);
    return renderEffect(call, local, file);
  });
  return { context, changes, render: () => effects.forEach(effect => effect.render()), stop: () => effects.forEach(effect => effect.stop()) };
}

test("agent locale metadata refresh preserves user defaults, while switching agents initializes new defaults", async () => {
  const state = agentEffects((path, locale) => Promise.resolve(path.startsWith("/api/agents/") ? {
    name: `agent-${locale}`, runtime_config: { default_count: path.endsWith("general_image") ? 2 : 4, default_creative_mode: "guided", generation_model_code: path.endsWith("general_image") ? "image" : "video" },
  } : {
    display_name: `model-${locale}`, category: path.endsWith("image") ? "image" : "video",
    default_params: { aspect_ratio: "16:9", quality: "2K", channel_key: path.endsWith("image") ? "image-channel" : "video-channel" },
    input_schema: { defaults: { duration: 6 } }, runtime_rule: {},
  }));
  state.render();
  await new Promise(setImmediate);
  assert.equal(state.changes.setCount, 2);
  assert.equal(state.changes.setBottom.channel_key, "image-channel");
  Object.assign(state.changes, { setCount: 7, setCreativeMode: "user-mode", setImageRatio: "1:1", setImageSize: "4K", setParams: { quality: "4K", custom: true }, setBottom: { channel_key: "user-channel" } });
  state.context.locale = "ja-JP";
  state.render();
  await new Promise(setImmediate);
  assert.equal(state.changes.setWorkflow.name, "agent-ja-JP");
  assert.equal(state.changes.setGenerationModel.display_name, "model-ja-JP");
  assert.equal(state.changes.setCount, 7);
  assert.equal(state.changes.setCreativeMode, "user-mode");
  assert.equal(state.changes.setImageRatio, "1:1");
  assert.equal(state.changes.setImageSize, "4K");
  assert.equal(state.changes.setParams.custom, true);
  assert.equal(state.changes.setBottom.channel_key, "user-channel");
  state.context.code = "ecommerce_video";
  state.render();
  await new Promise(setImmediate);
  assert.equal(state.changes.setCount, 4);
  assert.equal(state.changes.setCreativeMode, "guided");
  assert.equal(state.changes.setParams.duration, 6);
  assert.equal(state.changes.setParams.custom, undefined);
  assert.equal(state.changes.setBottom.channel_key, "video-channel");
  state.stop();
});

test("cancelled agent model initialization remains retryable in the next locale", async () => {
  let releaseModel;
  let firstModel = true;
  const state = agentEffects(path => {
    if (path.startsWith("/api/agents/")) return Promise.resolve({ runtime_config: { default_count: 2, generation_model_code: "image" } });
    if (firstModel) { firstModel = false; return new Promise(resolve => { releaseModel = resolve; }); }
    return Promise.resolve({ category: "image", default_params: { quality: "2K", channel_key: "retry-channel" }, runtime_rule: {} });
  });
  state.render();
  await new Promise(setImmediate);
  state.context.locale = "ja-JP";
  state.render();
  await new Promise(setImmediate);
  assert.equal(state.changes.setBottom.channel_key, "retry-channel");
  releaseModel({ category: "image", default_params: { quality: "4K", channel_key: "stale-channel" }, runtime_rule: {} });
  await new Promise(setImmediate);
  assert.equal(state.changes.setBottom.channel_key, "retry-channel");
  state.stop();
});

test("failed agent model initialization retries defaults and ignores results after unmount", async () => {
  let attempts = 0;
  let releaseModel;
  const state = agentEffects(path => {
    if (path.startsWith("/api/agents/")) return Promise.resolve({ runtime_config: { default_count: 2, generation_model_code: "image" } });
    if (++attempts === 1) return Promise.reject(new Error("offline"));
    return new Promise(resolve => { releaseModel = resolve; });
  });
  state.render();
  await new Promise(setImmediate);
  assert.equal(state.context.modelDefaultsCodeRef.current, "", "failure must not mark model defaults as initialized");
  state.context.locale = "ja-JP";
  state.render();
  await new Promise(setImmediate);
  assert.equal(attempts, 2);
  state.stop();
  releaseModel({ category: "image", default_params: { channel_key: "unmounted-channel" }, runtime_rule: {} });
  await new Promise(setImmediate);
  assert.equal(state.changes.setBottom.channel_key, "initial");
  assert.equal(state.context.modelDefaultsCodeRef.current, "");
});

test("model translated metadata preserves drafts while real schema, runtime and initial prompts still initialize", () => {
  const file = ts.createSourceFile("model.tsx", read("./workbench/ModelWorkspace.tsx"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  let keyExpression;
  function visit(node) {
    if (ts.isVariableDeclaration(node) && node.name.getText(file) === "initializationKey") keyExpression = node.initializer.getText(file);
    ts.forEachChild(node, visit);
  }
  visit(file);
  assert.ok(keyExpression);
  const call = effectCallFor('const secondaryKey = parseAudioRuntime', file);
  const changes = { setBottom: {} };
  const context = {
    useMemo: callback => callback(), initializedParamsKeyRef: { current: "" }, initialPrompt: "initial prompt",
    model: { code: "image", default_params: { count: 2 }, runtime_rule: { upstream: { static: { label: "business-value" } }, audio: { prompt_hint: "朗读" }, video: { frames: { first: { key: "first_frame", label: "首帧" } } } } },
    workbenchInputSchema: { properties: { label: { type: "string", title: "标签", enum: ["a", "b"], "x-enum-labels": ["甲", "乙"], default: "a" } } },
    isVideo: false, isAudio: false, isImage: true, capDeepThink: false, reasoningConfig: { default_enabled: false }, imageAllowsAutoRatio: false, imageCountMax: 10,
    parseAudioRuntime: () => ({}), schemaProperties: schema => schema.properties,
    schemaDefaults: schema => Object.fromEntries(Object.entries(schema.properties).map(([key, prop]) => [key, prop.default ?? prop.enum?.[0]])),
    EMPTY_VIDEO_MEDIA: {}, defaultImageSizeForConfig: () => "1K", normalizeRatio: value => value,
  };
  for (const [, setter] of call.arguments[0].getText(file).matchAll(/\b(set[A-Z]\w*)\(/g)) context[setter] = value => {
    changes[setter] = typeof value === "function" ? value(changes[setter]) : value;
  };
  const effect = renderEffect(call, context, file);
  const render = () => {
    vm.runInNewContext(ts.transpileModule(`globalThis.initializationKey = ${keyExpression}`, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
    effect.render();
  };
  render();
  Object.assign(changes, { setParams: { label: "user-label" }, setPrompt: "user draft", setImageCount: 7, setRefImages: ["reference"], setBottom: { channel_key: "user-channel" } });
  context.model = structuredClone(context.model);
  context.workbenchInputSchema = structuredClone(context.workbenchInputSchema);
  context.workbenchInputSchema.properties.label.title = "ラベル";
  context.workbenchInputSchema.properties.label["x-enum-labels"] = ["A", "B"];
  context.model.runtime_rule.audio.prompt_hint = "読み上げ";
  context.model.runtime_rule.video.frames.first.label = "最初のフレーム";
  render();
  assert.equal(changes.setParams.label, "user-label");
  assert.equal(changes.setPrompt, "user draft");
  assert.equal(changes.setImageCount, 7);
  assert.deepEqual(changes.setRefImages, ["reference"]);
  assert.equal(changes.setBottom.channel_key, "user-channel");
  context.workbenchInputSchema.properties.label.enum = ["new-business-value"];
  context.workbenchInputSchema.properties.label.default = "new-business-value";
  render();
  assert.equal(changes.setParams.label, "new-business-value", "named label business property must remain part of the signature");
  changes.setRefImages = ["new reference"];
  context.model.runtime_rule.upstream.static.label = "different-business-value";
  render();
  assert.equal(changes.setRefImages.length, 0, "upstream static label is a request field, not presentation");
  changes.setPrompt = "another draft";
  context.initialPrompt = "new preset prompt";
  render();
  assert.equal(changes.setPrompt, "new preset prompt");
  context.model.code = "other-model";
  context.model.default_params = { count: 3 };
  render();
  assert.equal(changes.setImageCount, 3);
  effect.stop();
});

test("direct agent route locale refresh preserves filled form and agent changes initialize it", async () => {
  const file = ts.createSourceFile("route.tsx", read("../app/app/agents/[code]/page.tsx"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const context = {
    code: "first", locale: "zh-CN", initializedFormCodeRef: { current: "" }, AbortController,
    schemaDefaults: schema => schema.defaults,
    apiForLocale: (path, locale) => Promise.resolve({ name: `agent-${locale}`, input_schema: { defaults: { prompt: `default-${path}` } } }),
    setWorkflow: value => { context.workflow = value; }, setForm: value => { context.form = value; },
  };
  const calls = [effectCallFor('initializedFormCodeRef.current = ""', file), effectCallFor("apiForLocale<Workflow>", file)];
  const effects = calls.map(call => renderEffect(call, Object.create(context), file));
  const render = () => effects.forEach(effect => effect.render());
  render();
  await new Promise(setImmediate);
  context.form = { prompt: "user draft", custom: "selection" };
  context.locale = "ko-KR";
  render();
  await new Promise(setImmediate);
  assert.equal(context.workflow.name, "agent-ko-KR");
  assert.equal(context.form.prompt, "user draft");
  assert.equal(context.form.custom, "selection");
  context.code = "second";
  render();
  await new Promise(setImmediate);
  assert.equal(context.form.prompt, "default-/api/agents/second");
  assert.equal(context.form.custom, undefined);
  effects.forEach(effect => effect.stop());
});

test("closed dialogs and specialized outputs are excluded from eager workspace imports", () => {
  const files = [
    ["./AppShell.tsx", ["RechargeModal"]],
    ["./workbench/ModelWorkspace.tsx", ["PricingModal"]],
    ["./workbench/BottomBar.tsx", ["SystemAssetLibraryDialog"]],
    ["./workbench/AgentWorkspace.tsx", ["SystemAssetLibraryDialog", "DetailPageRevisionEditor", "NovelChapterList"]],
    ["../app/app/agents/[code]/page.tsx", ["NovelWorkshopLanding", "NovelChapterList", "PhotoStudioLanding", "VirtualTryOnLanding"]],
    ["../../../admin/src/app/admin/models/page.tsx", ["ModelRoutesEditor", "UpstreamIncludeEditor"]],
  ];
  for (const [file, modules] of files) {
    const source = read(file);
    const parsed = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    for (const statement of parsed.statements) {
      if (ts.isImportDeclaration(statement) && !statement.importClause?.isTypeOnly) {
        assert.ok(!modules.some(module => statement.moduleSpecifier.text.endsWith(`/${module}`)), `${file}: eager import ${statement.moduleSpecifier.text}`);
      }
    }
    for (const module of modules) assert.match(source, new RegExp(`import\\("[^"\\n]*/${module}"\\)`));
  }
  assert.match(read("./AppShell.tsx"), /showRecharge && <RechargeModal/g);
  assert.match(read("./workbench/ModelWorkspace.tsx"), /pricingOpen && <PricingModal/);
  assert.match(read("./workbench/BottomBar.tsx"), /assetOpen && <SystemAssetLibraryDialog/);
});
