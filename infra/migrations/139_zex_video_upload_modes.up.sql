-- Align only legacy Zex menus with their saved upload shape; keep prices/routes.
WITH legacy AS (
  SELECT m.*, COALESCE(NULLIF(runtime_rule#>>'{video,mode_param}', ''), 'generation_mode') AS mode_key,
    runtime_rule#>>'{video,upload_profile}' AS profile
  FROM models m WHERE runtime_rule#>>'{upstream,adapter}' = 'zex_video'
), modes AS (
  SELECT l.*, input_schema#>ARRAY['properties',mode_key] AS property,
    CASE
      WHEN profile='none' THEN '["text"]'::jsonb
      WHEN profile IN ('multi_ref','single_ref','first_frame','frame_pair') THEN
        (SELECT jsonb_agg(value ORDER BY position) FROM jsonb_array_elements_text(input_schema#>ARRAY['properties',mode_key,'enum']) WITH ORDINALITY AS e(value,position)
         WHERE value='text' OR (profile IN ('multi_ref','single_ref') AND value='reference')
           OR (profile IN ('first_frame','frame_pair') AND value='first_frame')
           OR (profile='frame_pair' AND value='first_last'))
      WHEN profile='seedance_2' THEN
        (SELECT jsonb_agg(value ORDER BY position) FROM jsonb_array_elements_text(input_schema#>ARRAY['properties',mode_key,'enum']) WITH ORDINALITY AS e(value,position) WHERE value<>'reference')
        || CASE WHEN (input_schema#>ARRAY['properties',mode_key,'enum']) ? 'reference' THEN
          COALESCE((SELECT jsonb_agg(value ORDER BY position) FROM (VALUES
            ('image',1),('video',2),('image_audio',3),('image_video',4),('video_audio',5),('image_video_audio',6)) AS c(value,position)
            WHERE (value NOT LIKE '%image%' OR COALESCE((runtime_rule#>>'{video,reference_images,max}')::int,(runtime_rule#>>'{video,max_reference_images}')::int,0)>0)
              AND (value NOT LIKE '%video%' OR COALESCE((runtime_rule#>>'{video,reference_videos,max}')::int,0)>0)
              AND (value NOT LIKE '%audio%' OR COALESCE((runtime_rule#>>'{video,reference_audios,max}')::int,0)>0)), '[]'::jsonb)
          ELSE '[]'::jsonb END
      ELSE input_schema#>ARRAY['properties',mode_key,'enum']
    END AS options
  FROM legacy l
  WHERE profile IN ('gateway_reference','seedance_2','multi_ref','single_ref','first_frame','frame_pair','none')
    AND input_schema#>>ARRAY['properties',mode_key,'title'] <> '素材组合'
    AND jsonb_typeof(input_schema#>ARRAY['properties',mode_key,'enum'])='array'
    AND (input_schema#>ARRAY['properties',mode_key,'enum']) <@ '["text","first_frame","first_last","reference"]'::jsonb
), configured AS (
  SELECT modes.*, CASE WHEN options ? (default_params->>mode_key)
    THEN default_params->>mode_key ELSE options->>0 END AS selected_mode
  FROM modes WHERE jsonb_array_length(options)>0
)
UPDATE models m
SET input_schema=jsonb_set(m.input_schema,ARRAY['properties',c.mode_key],c.property || jsonb_build_object(
      'title','素材组合','enum',c.options,'default',c.selected_mode,
      'enumLabels',COALESCE(c.property->'enumLabels','{}'::jsonb) || '{"text":"文生视频","first_frame":"首帧","first_last":"首尾帧","reference":"参考内容","image":"参考图片","video":"参考视频","image_audio":"图片 + 音频","image_video":"图片 + 视频","video_audio":"视频 + 音频","image_video_audio":"图片 + 视频 + 音频"}'::jsonb)),
    default_params=COALESCE(m.default_params,'{}'::jsonb) || jsonb_build_object(c.mode_key,c.selected_mode),
    runtime_rule=jsonb_set(m.runtime_rule,'{video}',m.runtime_rule->'video' || jsonb_build_object(
      'upload_profile',CASE WHEN c.profile='gateway_reference' THEN 'aliyun_multimodal' ELSE c.profile END,
      'max_reference_total',COALESCE(m.runtime_rule#>'{video,max_reference_total}',to_jsonb(CASE WHEN m.new_api_model IN ('minimax-h3','minimax-h3-max','seedance-2.0','seedance-2.0-mini') THEN 9 ELSE COALESCE((m.runtime_rule#>>'{video,max_reference_images}')::int,4) END)),
      'reference_max_duration',COALESCE(m.runtime_rule#>'{video,reference_max_duration}',to_jsonb(CASE WHEN m.new_api_model='grok-imagine-video-1.5-fast' THEN 10 ELSE 0 END)))),
    updated_at=now()
FROM configured c WHERE m.id=c.id;
