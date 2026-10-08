# New API 多媒体兼容与计费配置

本方案扩展 StarAI 已有的模型、线路、参数 Schema 和异步任务能力，覆盖 Grok、Doubao、Qwen、Wan、Vidu、MiniMax 的已公开多媒体协议，并允许管理员配置 New API 二开接口。协议模板与可用模型分开：模板不代表网关已开通该模型，模型 ID、路径、分组权限和售价应以实际网关为准。

## 文档分析

核对日期：2026 年 10 月 4 日。

- [OpenLux Python 快速开始](https://doc.openlux.ai/tutorials/quickstart-python)说明 OpenAI SDK 使用 `https://api.openlux.ai/v1`，模型名以控制台为准。该网页是动态门户，本次通过其公开教程接口读取正文。
- [OpenLux 异步任务说明](https://doc.openlux.ai/tutorials/async-task-general)要求提交、查询、获取产物三个步骤配对；HTTP 200 也可能包含业务错误。不同协议可能返回 `task_id`、`data.task_id` 或 `taskBatchId`。
- [New API 图片](https://docs.newapi.ai/zh/docs/api/ai-model/images/openai/post-v1-images-generations)使用 `/v1/images/generations`，产物可为 URL 或 Base64，用量包含 input/output/total tokens。
- [New API 语音](https://docs.newapi.ai/zh/docs/api/ai-model/audio/openai/createspeech)使用 `/v1/audio/speech`，请求是 JSON，正常响应是音频二进制。
- [New API 通用视频](https://docs.newapi.ai/zh/docs/api/ai-model/videos/createvideogeneration)与 [Sora 格式](https://docs.newapi.ai/zh/docs/api/ai-model/videos/sora/createvideo)属于不同协议；JSON 的 `/v1/video/generations` 与 multipart 的 `/v1/videos` 分开配置。
- [xAI 图片](https://docs.x.ai/developers/rest-api-reference/inference/images)与 [视频](https://docs.x.ai/developers/rest-api-reference/inference/videos)采用不同的请求和任务查询结构；视频实际时长来自 `video.duration`。
- [Vidu 参考生图](https://platform.vidu.cn/docs/reference-to-image)和 [文生音效](https://platform.vidu.cn/docs/text-to-audio)均通过 creations 查询结果，分别支持参考图片和音效时长。
- [MiniMax 官方图片调用示例](https://github.com/MiniMax-AI/skills/blob/main/skills/frontend-dev/scripts/minimax_image.py)确认 `image-01` 使用 `/v1/image_generation`，响应图片 URL/Base64 均为数组；已添加对应原生和网关图片模板。

OpenLux 部分原生参考页存在正文和端点不一致的示例。涉及原生参数时再核对厂商文档，不复制明显错配的聊天示例。

## 高级 input_schema 与工作台（2026-10-08 审计）

- 修改 `properties.<参数>.default` 时同步到 `default_params`，报价和任务提交使用相同默认参数。未修改的 schema 字段保留现有 `default_params` 覆盖值，兼容普通表单设置；新增字段缺少默认参数时使用 schema 默认值。非法枚举、类型、范围或步长在保存时拒绝。
- 普通字段支持 `type`、`enum`、`enumLabels` / `x-enum-labels`、`title`、`description`、`placeholder`、`minimum` / `maximum` / `multipleOf`、`minLength` / `maxLength`。布尔枚举保持布尔类型，数组/对象通过现有文本控件编辑 JSON。`required`、数组数量约束由媒体任务 API 校验。
- `x-widget` 支持 `option_menu`、`select`、`boolean_toggle`、`textarea`，兼容旧 `widget`；`x-order`、`x-placement: top / audio_top` 控制顺序和位置，视频/音频的 `x-group: settings` 收入现有设置菜单。图标和强调样式沿用现有控件支持的 `x-icon` / `x-highlight`。
- 图片 schema 中声明的数量、比例、尺寸和质量使用现有 schema 控件，避免被固定工具栏覆盖；未声明的字段继续使用原有图片工具栏。显式像素尺寸同时用于报价和任务提交。
- 数量选项以 schema 为准，并受 `runtime_rule` 的数量上限约束；有枚举时，额外自定义数量需要 `x-allow-custom: true` 且运行规则允许。Veo/Omni 等原生模板可收窄支持的尺寸枚举、修改显示配置；固定协议不支持的选项不能通过 JSON 开启。
- 上传形态、素材容量、上游字段转发和价格分别仍由 `runtime_rule.video/audio/image`、`runtime_rule.upstream.include/map/static` 和 `price_rule` 管理。仅添加 schema 字段不会扩展上游能力；音频任务维持单结果。现有编辑器不是完整 JSON Schema 引擎，`$ref`、`oneOf`、`if/then/else` 等组合规则不驱动工作台的动态布局。

## 实施选择

1. 复用 `runtime_rule.upstream.include/map/static` 与独立线路连接配置。标准兼容、厂商原生和二开配置共用任务执行器。
2. 原生多媒体使用明确的 adapter，避免自动套用 Sora 参数清洗而删除厂商字段。保留管理员显式轮询路径。
3. 支持响应字段与状态映射；大任务 ID 保留十进制精度。异步音频和视频在必要时通过文件 ID 再检索下载地址。
4. 任务创建时保存售价快照；报价和创建共用数量、时长归一化。实际用量只接受服务端取得的上游响应。
5. 按次、按秒、按 Token 的真实计费设置独立于界面展示。参数控件复用现有主题，补充普通文本与数字字段。

## 协议范围

### 章鱼哥 Grok / Seedance / MiniMax 视频网关（2026-10-08）

根据 [Grok 创建](https://6l0ket291i.apifox.cn/521606507e0)、[Seedance 创建](https://6l0ket291i.apifox.cn/521606855e0)、[MiniMax 创建](https://6l0ket291i.apifox.cn/521606932e0)新增 8 个「章鱼哥 · … 视频 JSON」模板。提交均为 `POST /v1/videos`，查询为 `GET /v1/videos/{id}`，使用 Bearer 鉴权。

- Grok：`grok-imagine-video-1.5`、`-fast`、`-lite`。标准版支持首尾帧及最多 7 张参考图；Fast 仅支持单张首帧，多图参考最长 10 秒；Lite 仅支持文生和单张首帧。时长为 4～15 秒。
- Seedance：`seedance-2.0`、`seedance-2.0-mini` 支持 4～15 秒、图片/视频/音频合计最多 9 项；Seedance 2.5 合并为一个「章鱼哥 · seedance-2.5（10/15/30 秒）视频 JSON」模板，工作台选择时长后，分别调用 `seedance-2.5-10s`、`seedance-2.5-15s`、`seedance-2.5-30s`。2.5 只支持图片，按文档建议提供最多 4 张参考图；首尾帧为 1～2 张。
- MiniMax：`minimax-h3`、`minimax-h3-max` 均支持 4～15 秒、图片/视频/音频参考合计最多 9 项。该网关按参考内容处理素材，不提供原生 V2 首尾帧模式。

`zex_video` adapter 将平台 `duration` 转为字符串 `seconds`，将参考素材转为 `images`、`videos`、`audios` 数组，首帧/首尾帧按顺序写入 `images` 并设置 `first_last_frame: true`。按 2026-10-08 提供的上游配置截图，Grok、Seedance 2.0 / 2.5 模板分辨率为 `480p` / `720p`，MiniMax H3/H3 Max 为 `480p` / `768p`，默认 `480p`。Seedance 2.5 的统一模板也由用户选择分辨率，Worker 将所选值传给上游。工作台和画布读取 `input_schema.properties.resolution.enum`，API 使用同一枚举校验。后续上游扩展时，管理员修改该枚举及默认参数即可，无需修改前端代码；截图未启用的 1080p/2K/4K 不在初始模板中开放。Grok 和 Seedance 2.5 不允许视频/音频参考；超过数量、时长或模式限制的任务在冻结费用前拒绝。

默认使用 JSON；如需表单，在 `runtime_rule.upstream.request_format` 填写 `multipart`，Worker 会把所有素材按图片、视频、音频的顺序发送为重复的 `input_reference` 字段。平台上传的文件先转存为素材地址；视频和音频必须配置上游可访问的公网直链。

上传形态复用已有组件：新章鱼哥模板默认使用 `aliyun_multimodal` 对应的「首尾帧 / 多模态素材组合」，不是另建网关上传界面。选择 `seedance_2` 时使用现有 Seedance 组合组件，并按模型允许的图片/视频/音频数量生成组合选项；首尾帧仍使用现有首帧、尾帧槽位。选择 `multi_ref` / `single_ref` 时保留模型支持的文生、首帧、首尾帧和参考图片选项，参考模式仅接收图片；`frame_pair` / `first_frame` 只提供对应帧模式，`none` 只提供文生。Grok Fast/Lite、MiniMax 等不支持的模式不因切换上传形态而开放。工作台返回前台时重新读取模型配置，避免后台修改后仍使用旧菜单。

章鱼哥 MiniMax H3 / H3 Max 应选择「多模态素材组合（章鱼哥 MiniMax）」，内部仍复用 `aliyun_multimodal`。素材菜单展开文生视频、图片/视频/音频 + 文本、图片 + 音频、图片 + 视频、视频 + 音频、图片 + 视频 + 音频，并保留「自由参考组合」兼容按实际素材决定参考类型。上传槽位和资产库跟随组合显示，切换后隐藏的旧素材不进入提交参数或共享额度；后台/API 在冻结费用前校验组合所需素材，图片、视频、音频共享最多 9 项。输入区沿用现有主题、素材在上提示词在下的完整宽度布局，去掉合计数量说明，只保留上传入口数量显示与实际额度校验。菜单优先使用模型配置的标签，避免把自由组合误标成参考图。章鱼哥 MiniMax 不使用官方 V2 专用 `minimax_h3` 形态，不开放首帧/首尾帧模式。迁移 `143_zex_minimax_material_combinations.up.sql` 扩展旧的简化菜单，并将误选首帧形态的旧配置修复为通用组合，保留现有价格、线路、分辨率及数量限制；本地已应用并备份。

后台切换上传形态只同步素材菜单和默认模式，保留接口 adapter、模型名、连接、售价、成本、素材总数限制及字段映射。`seedance_2` 的图片/视频/音频组合在章鱼哥调用时仍映射成该网关的 `images/videos/audios`，不发送火山人像资产或样片任务 ID。工作台、画布、Agent 共用任务参数构造；模式切换后，隐藏素材及其资产引用不会混入提交。Worker 按配置的首尾帧字段取值，维持首帧在前、尾帧在后，并设置 `first_last_frame=true`。

组合素材共用 `max_reference_total` 总额度（章鱼哥默认 9 项），同时遵守后台各类素材的单独上限。工作台上传区与资产库按当前组合中已有图片、视频、音频数量计算剩余额度，并显示合计数量；例如已有 4 张图片时，音频最多还能添加 5 项。删除素材后恢复额度，隐藏素材不占当前组合额度。超过剩余额度的批量选择在上传前拦截，提交前再次校验；API 在冻结费用前校验素材总数，避免绕过前端产生超量任务。

迁移 `142_zex_seedance2_reference_image_limit.up.sql` 修复旧 Seedance 2.0 / Mini 配置中 `max_reference_images=9` 但 `reference_images.max=1` 的矛盾，仅处理多参考上传形态，保留显式单参考形态、自定义数量、素材总额度与价格。后台章鱼哥模板的“参考图最多”同步写入两处图片上限，移除重复的参考图槽位编辑项；首帧和尾帧各自仍限 1 张。组合参考图片的首次上传支持批量多选，后续添加和资产库同样遵守共享额度。2026-10-08 已在本地应用该迁移并保存旧配置备份；部署时需更新 Web/Admin 并执行迁移。

迁移 `139_zex_video_upload_modes.up.sql` 修复旧章鱼哥模板中上传形态与素材菜单不一致的问题，保留管理员已选择的形态及价格配置；旧 `gateway_reference` 形态转为现有组合组件。迁移仅处理旧菜单，已有新版或自定义组合菜单不覆盖，down 为 no-op。2026-10-08 在本地已执行并保存修改前配置备份；生产部署需执行该迁移并更新 API/Worker/Admin/Web。

迁移 `140_zex_seedance25_default_resolution.up.sql` 将固定时长 Seedance 2.5 的旧共享 `480p/720p` 菜单及默认值修正为 `auto`，保留素材组合、10/15/30 秒路由与售价，已有自定义分辨率菜单不覆盖。`auto` 仅保证本项目不发送 `resolution`，不能阻止中转网关补参。2026-10-08 的实际调用中，`480p` 与 `auto` 两次请求均失败；章鱼哥插件 11111 v1.0.14 为该模型补入默认 `480p`，渠道 48 转发到 `https://6853258.xyz` 后返回 HTTP 422 `resolution_conflict`。公开配置没有公布期望的固定值，不能仅凭本地模拟测试认定问题已解决。两次本地任务实际扣费为 0、冻结费用均已释放，上游日志费用也为 0。需核实最终模型的固定档位，再通过现有输入枚举、默认参数或线路参数配置相符的值，不自动重试付费任务。

Worker 在截断错误之前解析中转层嵌套的 JSON `message/error`，保留最终供应商错误码与说明。多层 `fail_to_fetch_task` 包装现在显示为 `resolution_conflict: 请求的 resolution 与模型固定参数不一致`，不再丢失实际原因。

根据用户最终选择，迁移 `141_zex_seedance25_resolution_selection.up.sql` 将迁移 140 的 `auto` 菜单恢复为 `480p` / `720p`，默认 `480p`，移除「上游默认」标签。新模板使用同样配置；已有自定义分辨率菜单不覆盖，素材组合、时长路由及三个时长的固定售价保持原配置。当前配置以迁移 141 为准，本地已应用并备份旧配置。

响应 `id` 为任务 ID，`queued` / `in_progress` 继续轮询，`completed` 读取顶层 `url` 或 `video_url`，`failed` 读取上游错误。`response_map.media_url` 可用路径数组指定优先顺序，缺失产物时不会将回显参考图当作结果。Seedance 2.5 默认按所选时长档位收取固定费用，Seedance 2.0 默认按所选分辨率按次计费，其他章鱼哥模板默认按秒计费。管理员仍可切换现有计费方式；按秒计费未返回真实时长时沿用已有转存后测量/估算机制。

章鱼哥 Seedance 2.0 / Mini 的后台提供 `480p`、`720p` 独立单价，价格写入 `price_rule.unit_price_by_resolution`，线路成本写入 `cost_rule.unit_cost_by_resolution`。两者在 `per_request` 下表示每次固定金额，在 `per_second` 下表示每秒单价；按次不乘视频时长，供应商成本按实际上游请求次数计算。模型售价与线路成本金额分别填写，模板不将上游公布的价格自动用作用户售价。新模板的售价初始为 0，已有模型未配置分辨率映射时沿用原单位价格；编辑其中一档会保留另一档原价。报价、任务冻结、Worker 结算及 Agent/画布任务复用相同分档规则，任务售价快照不受后来改价影响。按 Token、动态规则继续使用已有对应配置；Seedance 2.5 的时长固定价规则保持原有优先级。

章鱼哥 MiniMax H3 / H3 Max 默认按秒计费，后台分别提供 `480p`、`768p` 与「其他情况（Other cases）」的用户售价和线路上游成本。分辨率档位复用上述映射，Other cases 使用 `unit_price` / `unit_cost`，在分辨率未匹配已配置档位时兜底，并非额外的分辨率选项。后续扩展分辨率菜单会自动出现对应价格字段。已有模型即使没有分辨率价格映射，也能直接编辑，无需重新应用模板；修改任一档或兜底价格时保留其他档位原来的有效价格。新模板各档售价初始为 0，不覆盖已保存的售价。工作台预估、Agent 报价、任务冻结、价格快照、Worker 结算及线路成本继续复用现有分档计费链路，按匹配档位的每秒单价乘时长。

Seedance 2.5 使用 `runtime_rule.upstream.model_template: "seedance-2.5-{duration}s"`。管理员应用统一模板后，在视频计费区域填写 10、15、30 秒三个固定售价，在线路成本区域填写对应的三个上游固定成本。售价存于 `price_rule.unit_price_by_duration`，成本存于线路 `cost_rule.unit_cost_by_duration`，两者均使用 `per_request`，键为 `"10"`、`"15"`、`"30"`。模板初始三档售价均为 0，需按实际业务填写。报价、冻结及成功结算按用户提交时选择的时长匹配售价，并沿用任务创建时的价格快照；产物测量时长不改变固定档位。供应商成本按对应档位单价与实际生成请求次数计算。没有时长映射的旧按次配置仍使用原有 `unit_price` / `unit_cost`。

这些模板与原生 Grok、Seedance、MiniMax 模板分别保留。更新服务后，管理员需选择「章鱼哥」对应型号模板并配置实际网关连接、售价及线路成本；不会自动修改已有模型或收费配置。

| 系列 | 图片 | 视频 | 音频 |
| --- | --- | --- | --- |
| Grok | OpenAI 兼容图片 | xAI 请求 ID、`video.url` 与显式查询路径 | 只配置网关实际支持的语音协议 |
| Doubao | Seedream 图片参数 | Seedance 官方 adapter 或网关统一视频 | 网关实际支持的语音协议 |
| Qwen | OpenAI 兼容或现有阿里原生图片 | 按实际视频模型单独配置 | 现有 Qwen TTS 或网关语音 |
| Wan | 复用实际开放的图片协议 | 阿里历史版图生视频与统一视频分开 | 不宣称不存在的音频生成能力 |
| Vidu | 1–7 张参考图片的异步参考生图 | `task_id`、状态、`creations` 列表 | 2–10 秒文生音效及网关开放的语音协议 |
| MiniMax | Image-01 原生文生图或网关 Images JSON | 异步查询及文件检索 | 同步语音、异步语音与音乐分别配置 |

## 计费约定

- `per_request`：延续现有售价语义，一次平台任务收取一次单价。批量实际发出的多个上游请求可产生多份线路成本；售价与供应商成本不混用。
- `per_image`：预估按请求张数冻结，成功按实际持久化图片张数结算；一次 POST 的 `n=3` 不等于三次上游调用。线路按次成本使用 Worker 记录的真实生成请求次数。
- `per_second`：按生成时长与数量估算；优先使用上游实际生成秒数，缺失时使用 ffprobe 测量转存后的音频/视频，最多共等待 15 秒。测量失败保留预估，并在 `output.billing_usage.seconds_source` 标明 `estimated`。上游提供的聚合时长不可再次乘数量。
- `per_token`：输入与输出分别按每百万 Token 单价计算。总量是分项之和，不将 total 同时再算作 output；聚合用量不可再次乘批次数。
- 创建时冻结、成功结算、失败释放继续使用钱包锁和事务完成。公开请求不能注入 `_actual_*`、`_estimated_*` 或价格快照等内部账务字段。
- 钱包、冻结与任务费用沿用数据库六位小数：所有正数冻结向上取六位，实际费用按六位舍入；低于最小单位的实际费用可为零并完整释放。线路成本保持数据库八位精度。NaN、Infinity 与运算溢出均拒绝。
- 无可信实际用量的接口沿用预估金额；此时界面和部署文档必须明确其属于估算。模板不内置猜测的商业价格，管理员需填写售价和成本。
- 保留现有钱包规则：实际金额超过冻结估价时补扣差额，余额可变为负数。冻结估价不是扣费上限；如需硬性限制，需要另行定义任务预算与上游取消策略。

## 二开配置方法

在后台「模型管理」选择分类及「上游接口模板」，填写实际 Base URL、鉴权、模型别名、售价与线路成本。生成 Endpoint、查询 Endpoint 必须属于同一协议。不要只替换模型名称而继续使用另一厂商的请求字段。

`runtime_rule.upstream` 可配置 `include`、`map`、`static`、`request_format`、`poll_path`、`response_map`、`success_statuses`、`failure_statuses`、`result_path`、`result_response_map`。路径以实际网关为准，例如 MiniMax 二开可能需要 `/minimax/v1/...` 前缀。

```json
{
  "upstream": {
    "adapter": "native_media",
    "request_format": "json",
    "include": ["duration", "first_frame"],
    "map": {"first_frame": "image.url"},
    "poll_path": "/custom/jobs/{id}",
    "response_map": {
      "task_id": "data.job_id",
      "status": "data.state",
      "media_url": "data.outputs.0.url",
      "output_seconds": "data.output_seconds",
      "usage": "data.usage"
    },
    "success_statuses": ["succeed"],
    "failure_statuses": ["failed"]
  }
}
```

字段路径支持点号与数组下标。`native_media` 显式空 `include: []` 仅发送基础 model/prompt；未配置 include 延续旧模板行为。显式产物映射缺失时不会把请求回显的参考图片当作生成结果。任务 ID 使用原始数字精度。

MiniMax `extra_info.audio_length` 等毫秒字段使用 `output_seconds_scale: 0.001`。文件检索 `result_path` 仅允许相同网关源，防止鉴权信息被发送到其他源；文件检索与产物下载分别处理 JSON 和二进制。

Grok 图片编辑协议与 OpenAI multipart 编辑不同，本次新增的是原生文生图。复杂 Doubao/Qwen 图片尺寸、参考图和原生语音仍使用系统已有厂商模板，或按网关文档修改 include/map；通用兼容图片模板默认只启用文生图。

## 上线配置

1. 部署更新后的 API、Worker、Admin 和 Web；28 个多媒体模板在后台代码中提供，不会自动向业务库创建已启用的收费模型。已有 Seedance 2.5 模型需重新应用统一模板并配置三档价格，才能使用时长切换。
   分辨率修复通过迁移 `138_zex_video_resolution_options.up.sql` 补齐旧版章鱼哥模板自由输入字段，保留有效默认分辨率及所有价格、线路设置；已配置枚举的模型不覆盖。不需要为分辨率修复重新应用模板。该数据修复的 down 为 no-op，避免回滚时抹掉后续管理员配置。
2. 配置已授权的网关连接、模型别名、生成及查询路径、售价、线路成本、参数范围和上传能力。模板初始售价为 0。
3. Worker 保持 ffmpeg/ffprobe 可用，配置可访问的对象存储或本地存储公开地址，才能转存并测量产物。
4. 运行数据库回归时设置 `MEDIA_BILLING_TEST_DATABASE_URL`、`TASK_READ_TEST_DATABASE_URL`，指向专用测试库；测试会创建并清理独立 schema。

真实供应商验收还需用已授权令牌逐个核验图片、视频、语音任务：成功结果、失败退款、实际用量、上传约束、查询权限和文件链接有效期。自动化模拟只能证明已测试的协议形状及本地链路，不能证明某个网关已提供指定型号或真实售价。

## 章鱼哥链路审计（2026-10-08）

审计覆盖后台模板应用、工作台/画布参数、API 参数校验与报价冻结、Worker 提交/查询/转存、成功结算及失败释放。

本次修复：

- 后台任务列表的模型名称查询改为实际字段 `models.display_name`，避免新增模型列导致列表 SQL 失败。
- 章鱼哥任务已保存上游任务 ID 后，队列重新投递只查询原任务、原线路，不再次提交生成；完成后的重复投递忽略，不重复扣费。手动重试失败任务会清除旧任务 ID 和线路，作为新生成重新报价、冻结。
- 提交或查询返回明确失败状态时直接失败并释放冻结金额。即使错误文字包含 timeout，也不自动重试生成。
- 参考视频/音频中的 localhost、MinIO 服务名及私网 IP 在冻结前拒绝；私网图片仍走 Worker 读取并转为图片数据的已有流程。公网域名的实际可访问性由网关决定，校验不做 DNS/远程探测。

验证结果：API Service/Billing、Worker VideoParams/执行器测试通过；后台模板与工作台/画布相关 102 个 Node 回归通过。实际执行本地 PostgreSQL 隔离回归，验证三档固定售价、提交后改价的快照、对应线路成本、重复投递只结算一次、提交/查询明确失败释放冻结，以及后台模型列查询。网关使用本地模拟服务，未调用收费供应商；视频转存使用传输测试素材，不代表真实视频编解码验收。

复现数据库回归：`TASK_READ_TEST_DATABASE_URL` 和 `MEDIA_BILLING_TEST_DATABASE_URL` 用于 API 的任务查询与账务测试；`PRODUCT_TEST_DATABASE_URL` 用于 Worker 的 `TestZexResumedTaskBillingDatabase`。使用本地专用测试库；测试仅操作连接临时表或独立 schema，并清理自身数据。

剩余边界：供应商已接单，但提交响应在网络中丢失，或任务 ID 尚未成功持久化时进程退出，平台仍无法确定该次提交是否成功。当前网关协议未提供已验证的幂等提交能力，因此这一窗口仍可能产生供应商重复生成/计费；本次复用任务 ID 的修复覆盖已保存回执的任务。运行中的模型/线路连接变更、禁用或删除也可能使原任务无法继续查询，不能自动切换线路重新生成来补救。真实接单与收费规则仍需供应商验收。

### 上传、报价与多语言复核（2026-10-08 晚间）

复核了本地已配置的八个章鱼哥视频模型，包含 Grok 1.5 / Fast / Lite、Seedance 2.0 / Mini / 2.5、MiniMax H3 / H3 Max。读取模型及线路配置时不输出连接密钥，不修改管理员售价或用户钱包。

| 链路 | 复核结果 |
| --- | --- |
| 素材组合 | 使用现有上传组件，提交时按当前模式筛选素材；首帧、首尾帧和多参考模式分别处理。MiniMax 的参考组合不套用其他厂商首尾帧协议。 |
| 素材额度 | Seedance 2.0 的图片、视频、音频共用 9 项额度；Grok 的参考图额度及 Fast 多参考时长限制保持生效。 |
| 分辨率售价 | Seedance 2.0 支持按分辨率收取每次固定费用；MiniMax 支持分辨率每秒单价及其他情况兜底价。实际售价沿用管理员选择的计费类型与配置。 |
| 时长售价 | Seedance 2.5 由 10/15/30 秒定位模型，并使用对应档位固定售价，不将固定售价再乘以秒数。 |
| 冻结与结算 | 参数验证在冻结前执行；结算使用任务售价快照。隔离数据库验证成功结算一次、失败释放冻结、重复投递不重复扣费。 |
| 本地历史账务 | 此次检查未发现八个模型的终态任务残留冻结或同任务多笔支出。 |

发现并修复的工作台问题：

- Schema 默认时长原先覆盖后台默认值，例如后台设 5 秒、界面显示 4 秒。首次初始化及配置刷新现在都优先使用后台默认参数。
- 参数快速变化时，旧的费用预估响应可能覆盖新档位。现在合并 180ms 内的连续修改，取消过期请求并忽略旧响应；相同参数不会因无关重渲染重复报价。
- 浏览器回到工作台时，focus 与 visibilitychange 事件复用同一个进行中的模型刷新。
- 价格面板按当前语言加载并缓存模型信息，忽略切换语言或关闭后的旧响应。分类标签、分辨率计费说明及单位使用翻译入口。
- 图片、视频的预估尚未返回时，输入区显示实际计费模式，不再误用默认的「按 Token 计费」提示。
- 中文、英文、日文、韩文、越南文补齐新素材组合、价格与预估文案。动态数量提示改成完整的具名变量句子，保留此前对 Grok / MiniMax 隐藏额度说明的界面规则。自定义模型介绍仍依赖后台内容翻译配置。
- 翻译目录脚本纳入 `Object.assign` 补充键及源词典中的配置驱动文案，避免这些文案无法进入后台覆盖与回填目录。执行 `node scripts/sync-ui-translation-catalog.js --check` 可验证同步状态；同步脚本不调用收费翻译模型。

本次验证：Web/Admin TypeScript 检查、47 个前端/多语言回归，以及 API Service/Billing、Worker 执行器/VideoParams 测试通过；API 临时表和 Worker 独立 schema 的 PostgreSQL 回归实际执行通过。浏览器确认 MiniMax 默认 5 秒，在本地配置下 480p 预估 0.30 算力、768p 预估 0.40 算力，并检查素材组合与价格面板翻译。未提交真实收费生成任务。

本地开发环境一次预热后的服务端 HTML 响应：首页约 409ms、MiniMax 工作台约 83ms。这不代表完整浏览器加载时间，也不能推算线上或供应商生成耗时；Next.js 开发模式的首次编译仍会增加等待时间。

需要管理员补齐的配置：当前八条线路的 `cost_rule.unit_cost` 均为 0。用户售价有值且可独立扣费，但供应商成本与利润统计会缺少真实成本。按供应商合同、线路计费方式和系统币值填写实际成本；分辨率或时长分档成本可分别使用 `unit_cost_by_resolution` / `unit_cost_by_duration`，不要直接把供应商人民币售价当作用户算力售价。
