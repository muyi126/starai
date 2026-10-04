import assert from "node:assert/strict";
import test from "node:test";
import { MULTIMEDIA_TEMPLATES, applyMultimediaTemplate, clearTemplateConnection, clearMediaTemplateRuntime } from "./multimedia-templates.ts";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";

test("gateway templates retain credentials and explicit identity while resetting provider params and prices", () => {
  const current = { new_api_model: "custom-model-alias", runtime_rule: '{"upstream":{"adapter":"old","static":{"old":true}}}', new_api_extra_params: JSON.stringify({ connection: { base_url: "https://gateway.test", api_key: "test-key" }, custom: true }) };
  assert.equal(MULTIMEDIA_TEMPLATES.length, 20);
  assert.equal(new Set(MULTIMEDIA_TEMPLATES.map((item) => item.key)).size, MULTIMEDIA_TEMPLATES.length);
  for (const template of MULTIMEDIA_TEMPLATES) {
    const form = applyMultimediaTemplate(current, template.key);
    const runtime = JSON.parse(form.runtime_rule);
    const extra = JSON.parse(form.new_api_extra_params);
    assert.equal(extra.connection.base_url, "https://gateway.test");
    assert.equal(extra.connection.api_key, "test-key");
    assert.equal(extra.custom, true);
    assert.equal(runtime.template_key, template.key);
    assert.equal(runtime.upstream.static?.old, undefined);
    assert.equal(form.new_api_model, template.model || "custom-model-alias");
    assert.equal(JSON.parse(form.price_rule).unit_price, 0);
    assert.ok(template.endpoint.startsWith("/"));
    assert.ok(template.description);
  }
  assert.equal(applyMultimediaTemplate(current, "missing"), current);
});

test("audio display preference preserves per-second billing and switching preserves zero/token rates", () => {
  const source = ts.createSourceFile("models.tsx", readFileSync(new URL("../app/admin/models/page.tsx", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const pricing = source.statements.find((node) => ts.isFunctionDeclaration(node) && node.name?.text === "switchedPriceRule");
  let callback;
  function visit(node) {
    if (ts.isJsxOpeningElement(node) && node.tagName.getText(source) === "select") {
      const value = node.attributes.properties.find((attribute) => attribute.name?.text === "value");
      if (value?.getText(source).includes("getAudioRule(form.runtime_rule).billing_hint")) {
        callback = node.attributes.properties.find((attribute) => attribute.name?.text === "onChange").initializer.expression;
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(source);
  let form = { price_rule: '{"billing_type":"per_second","unit_price":0.2}', runtime_rule: "{}" };
  const context = { setForm: (update) => { form = update(form); }, setAudioRule: (runtime, patch) => JSON.stringify({ ...JSON.parse(runtime), audio: patch }) };
  vm.runInNewContext(ts.transpileModule(`${pricing.getText(source)}\nconst change = ${callback.getText(source)};`, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  vm.runInNewContext('change({target:{value:"estimated"}})', context);
  assert.equal(JSON.parse(form.price_rule).billing_type, "per_second");
  assert.equal(JSON.parse(form.price_rule).unit_price, 0.2);
  assert.equal(context.switchedPriceRule({ unit_price: 0 }, "per_request").unit_price, 0);
  assert.equal(context.switchedPriceRule({ input_price_per_m: 0, output_price_per_m: 0 }, "per_token").input_price_per_m, 0);
  assert.equal(context.switchedPriceRule({ input_price: 0.000002 }, "per_token").input_price_per_m, 2);
});

test("native protocols use exact request, poll, media and billing fields", () => {
  const byKey = Object.fromEntries(MULTIMEDIA_TEMPLATES.map((template) => [template.key, template]));
  assert.equal(byKey.newapi_video_json.runtime.upstream.map.first_frame, "image");
  assert.equal(byKey.gateway_video_multipart.runtime.upstream.request_format, "multipart");
  assert.equal(byKey.gateway_video_json.runtime.upstream.request_format, "json");
  assert.equal(byKey.gateway_video_json.runtime.upstream.result_path, undefined);
  assert.equal(byKey.grok_native_video.endpoint, "/v1/videos/generations");
  assert.equal(byKey.grok_native_video.runtime.upstream.response_map.output_seconds, "video.duration");
  assert.equal(byKey.minimax_native_video.runtime.upstream.result_response_map.media_url, "file.download_url");
  assert.equal(byKey.minimax_native_video.runtime.upstream.response_map.error, undefined);
  assert.equal(byKey.vidu_native_image_video.connection.auth_type, "token");
  assert.equal(byKey.vidu_native_image_video.runtime.video.prompt_required, false);
  assert.equal(byKey.vidu_native_image_video.schema.properties.aspect_ratio, undefined);
  assert.equal(byKey.vidu_native_image_video.runtime.upstream.include.includes("aspect_ratio"), false);
  assert.equal(byKey.vidu_native_text_video.runtime.video.prompt_required, true);
  assert.equal(byKey.vidu_reference_image.runtime.image.max_reference_images, 7);
  assert.equal(byKey.vidu_text_audio.billing, "per_second");
  assert.equal(byKey.vidu_text_audio.schema.properties.duration.maximum, 10);
  assert.equal(byKey.wan_native_image_video.runtime.upstream.map.first_frame, "img_url");
  assert.equal(byKey.newapi_audio_speech.schema.properties.speed.type, "number");
  assert.equal(byKey.newapi_minimax_image.model, "image-01");
  assert.equal(byKey.newapi_minimax_image.endpoint, "/v1/images/generations");
  assert.equal(byKey.minimax_native_image.endpoint, "/v1/image_generation");
  assert.equal(byKey.minimax_native_image.defaults.count, 1);
  assert.equal(byKey.minimax_native_image.defaults.aspect_ratio, "1:1");
  assert.equal(byKey.minimax_native_image.defaults.response_format, "url");
  assert.equal(byKey.minimax_native_image.runtime.image.max_reference_images, 0);
  assert.equal(byKey.minimax_native_image.runtime.upstream.map.count, "n");
  assert.deepEqual(byKey.minimax_native_image.runtime.upstream.include, ["count", "aspect_ratio", "seed", "response_format", "prompt_optimizer"]);
  assert.deepEqual(byKey.minimax_native_image.runtime.image.supported_ratios, ["1:1", "16:9", "4:3", "3:2", "2:3", "3:4", "9:16", "21:9"]);
  assert.equal(byKey.minimax_native_image.runtime.upstream.response_map.media_url, "data.image_urls");
  assert.equal(byKey.minimax_native_image.runtime.upstream.response_map.media_base64, "data.image_base64");
  assert.equal(byKey.minimax_native_image.runtime.upstream.response_map.error, undefined);
  assert.equal(byKey.minimax_native_image.runtime.image.count_max, 9);
  assert.equal(byKey.minimax_native_image.runtime.image.count_allow_custom, true);
  assert.deepEqual(byKey.minimax_native_image.schema.properties.count.enum, [1, 2, 3, 4, 5, 6, 7, 8, 9]);
  assert.equal(byKey.grok_native_video.schema.properties.seed, undefined);
  assert.deepEqual(byKey.newapi_audio_speech.schema.required, ["voice"]);
  for (const key of ["newapi_doubao_image", "newapi_qwen_image"]) assert.deepEqual(byKey[key].runtime.upstream.include, []);
});

test("template switches remove owned authentication, headers and response mapping but keep credentials and custom configuration", () => {
  const initial = { new_api_model: "alias", runtime_rule: "{}", new_api_extra_params: JSON.stringify({ connection: { base_url: "https://gateway.test", api_key: "test-key", headers: { "X-Custom": "keep" } } }) };
  const vidu = applyMultimediaTemplate(initial, "vidu_native_text_video");
  assert.equal(JSON.parse(vidu.new_api_extra_params).connection.auth_type, "token");
  const minimax = applyMultimediaTemplate(vidu, "minimax_native_image");
  assert.equal(JSON.parse(minimax.new_api_extra_params).connection.auth_type, "bearer");
  const wan = applyMultimediaTemplate(initial, "wan_native_video");
  const reset = JSON.parse(clearTemplateConnection(wan.new_api_extra_params, wan.runtime_rule));
  assert.deepEqual(reset.connection.headers, { "X-Custom": "keep" });
  assert.equal(reset.connection.base_url, "https://gateway.test");
  assert.equal(reset.connection.api_key, "test-key");
  const edited = JSON.parse(wan.new_api_extra_params);
  edited.connection.headers["X-DashScope-Async"] = "edited";
  assert.equal(JSON.parse(clearTemplateConnection(JSON.stringify(edited), wan.runtime_rule)).connection.headers["X-DashScope-Async"], "edited");
  const runtime = JSON.parse(clearMediaTemplateRuntime(JSON.stringify({ ...JSON.parse(minimax.runtime_rule), reasoning: { default_enabled: true } })));
  for (const key of ["template_key", "upstream", "image", "video", "audio"]) assert.equal(runtime[key], undefined);
  assert.deepEqual(runtime.reasoning, { default_enabled: true });
});

test("actual legacy OpenAI preset clears MiniMax response mapping after a template switch", () => {
  const source = ts.createSourceFile("models.tsx", readFileSync(new URL("../app/admin/models/page.tsx", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  let builder;
  function visit(node) {
    if (ts.isVariableDeclaration(node) && node.name.getText(source) === "buildImageEndpointPreset") builder = node.initializer;
    ts.forEachChild(node, visit);
  }
  visit(source);
  const context = { MULTIMEDIA_TEMPLATES, applyMultimediaTemplate, clearTemplateConnection, clearModelCaps: clearMediaTemplateRuntime, IMAGE_ENDPOINT_PRESETS: [{ key: "openai_images", endpoint: "/v1/images/generations", model: "gpt-image-1" }], ALIYUN_QWEN_IMAGE_TEMPLATE_KEY: "qwen", safeParseJson: JSON.parse, openAIImageSchema: () => "{}", OPENAI_IMAGE_QUALITIES: ["auto"] };
  vm.runInNewContext(ts.transpileModule(`const build = ${builder.getText(source)};`, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  context.current = applyMultimediaTemplate({ new_api_model: "", new_api_extra_params: "{}", runtime_rule: "{}" }, "minimax_native_image");
  const next = vm.runInNewContext('build(current, "openai_images")', context);
  const upstream = JSON.parse(next.runtime_rule).upstream;
  assert.equal(upstream.adapter, "openai_images");
  for (const key of ["response_map", "result_path", "include", "map"]) assert.equal(upstream[key], undefined);
});
