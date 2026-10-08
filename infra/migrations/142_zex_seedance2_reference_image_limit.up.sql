-- Repair the contradictory Seedance 2.0 preset: 9 reference images advertised,
-- but the older frame-slot editor persisted a one-image reference slot.
-- Preserve explicit single-reference profiles, other limits and prices.
UPDATE models
SET runtime_rule = jsonb_set(runtime_rule, '{video,reference_images,max}', '9'::jsonb),
    updated_at = now()
WHERE runtime_rule#>>'{upstream,adapter}' = 'zex_video'
  AND (runtime_rule->>'template_key' IN ('zex_seedance_2_0', 'zex_seedance_2_0_mini')
       OR new_api_model IN ('seedance-2.0', 'seedance-2.0-mini'))
  AND runtime_rule#>>'{video,upload_profile}' IN ('seedance_2', 'aliyun_multimodal', 'gateway_reference', 'multi_ref')
  AND runtime_rule#>'{video,max_reference_images}' = '9'::jsonb
  AND runtime_rule#>'{video,reference_images,max}' = '1'::jsonb;
