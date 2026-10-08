-- Expand legacy MiniMax reference menus using the existing multimodal component.
-- Preserve prices, routes, media limits and all other parameter fields.
WITH legacy AS (
  SELECT m.*, COALESCE(NULLIF(runtime_rule#>>'{video,mode_param}', ''), 'generation_mode') AS mode_key
  FROM models m
  WHERE runtime_rule#>>'{upstream,adapter}' = 'zex_video'
    AND (new_api_model IN ('minimax-h3','minimax-h3-max')
      OR runtime_rule->>'template_key' IN ('zex_minimax_h3','zex_minimax_h3_max'))
    AND runtime_rule#>>'{video,upload_profile}' IN ('aliyun_multimodal','gateway_reference','seedance_2','minimax_h3','first_frame','frame_pair')
), modes AS (
  SELECT l.*, input_schema#>ARRAY['properties',mode_key] AS property,
    '["text"]'::jsonb || COALESCE((
      SELECT jsonb_agg(value ORDER BY position) FROM (VALUES
        ('image',1),('video',2),('audio',3),('image_audio',4),('image_video',5),('video_audio',6),('image_video_audio',7),('reference',8)
      ) AS c(value,position)
      WHERE (value NOT LIKE '%image%' OR COALESCE((runtime_rule#>>'{video,reference_images,max}')::int,(runtime_rule#>>'{video,max_reference_images}')::int,0)>0)
        AND (value NOT LIKE '%video%' OR COALESCE((runtime_rule#>>'{video,reference_videos,max}')::int,0)>0)
        AND (value NOT LIKE '%audio%' OR COALESCE((runtime_rule#>>'{video,reference_audios,max}')::int,0)>0)
    ),'[]'::jsonb) AS options
  FROM legacy l
  WHERE jsonb_typeof(input_schema#>ARRAY['properties',mode_key,'enum'])='array'
    AND (input_schema#>ARRAY['properties',mode_key,'enum']) <@ '["text","reference","first_frame","first_last","last_frame"]'::jsonb
), configured AS (
  SELECT modes.*, CASE WHEN options ? (default_params->>mode_key)
    THEN default_params->>mode_key ELSE 'text' END AS selected_mode
  FROM modes
)
UPDATE models m
SET input_schema=jsonb_set(m.input_schema,ARRAY['properties',c.mode_key],c.property || jsonb_build_object(
      'title','素材组合','enum',c.options,'default',c.selected_mode,
      'enumLabels',COALESCE(c.property->'enumLabels','{}'::jsonb) || '{"text":"文生视频","image":"图片 + 文本","video":"视频 + 文本","audio":"音频 + 文本","image_audio":"图片 + 音频","image_video":"图片 + 视频","video_audio":"视频 + 音频","image_video_audio":"图片 + 视频 + 音频","reference":"自由参考组合"}'::jsonb)),
    default_params=COALESCE(m.default_params,'{}'::jsonb) || jsonb_build_object(c.mode_key,c.selected_mode),
    runtime_rule=jsonb_set(m.runtime_rule,'{video,upload_profile}','"aliyun_multimodal"'::jsonb),
    updated_at=now()
FROM configured c WHERE m.id=c.id;
