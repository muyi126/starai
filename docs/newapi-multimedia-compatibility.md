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

## 实施选择

1. 复用 `runtime_rule.upstream.include/map/static` 与独立线路连接配置。标准兼容、厂商原生和二开配置共用任务执行器。
2. 原生多媒体使用明确的 adapter，避免自动套用 Sora 参数清洗而删除厂商字段。保留管理员显式轮询路径。
3. 支持响应字段与状态映射；大任务 ID 保留十进制精度。异步音频和视频在必要时通过文件 ID 再检索下载地址。
4. 任务创建时保存售价快照；报价和创建共用数量、时长归一化。实际用量只接受服务端取得的上游响应。
5. 按次、按秒、按 Token 的真实计费设置独立于界面展示。参数控件复用现有主题，补充普通文本与数字字段。

## 协议范围

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

1. 部署更新后的 API、Worker、Admin 和 Web；20 个模板在后台代码中提供，不会自动向业务库创建已启用的收费模型。
2. 配置已授权的网关连接、模型别名、生成及查询路径、售价、线路成本、参数范围和上传能力。模板初始售价为 0。
3. Worker 保持 ffmpeg/ffprobe 可用，配置可访问的对象存储或本地存储公开地址，才能转存并测量产物。
4. 运行数据库回归时设置 `MEDIA_BILLING_TEST_DATABASE_URL`、`TASK_READ_TEST_DATABASE_URL`，指向专用测试库；测试会创建并清理独立 schema。

真实供应商验收还需用已授权令牌逐个核验图片、视频、语音任务：成功结果、失败退款、实际用量、上传约束、查询权限和文件链接有效期。自动化模拟只能证明已测试的协议形状及本地链路，不能证明某个网关已提供指定型号或真实售价。
