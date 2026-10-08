-- Fixed-duration Seedance 2.5 aliases use server defaults; the old shared
-- 480p/720p menu forced a value that could conflict with those fixed models.
-- Preserve custom resolution menus, upload shapes, routing and pricing.
UPDATE models
SET input_schema = jsonb_set(input_schema, '{properties,resolution}',
      input_schema#>'{properties,resolution}' ||
      '{"enum":["auto"],"default":"auto","enumLabels":{"auto":"上游默认"},"x-omit-auto":true}'::jsonb),
    default_params = COALESCE(default_params, '{}'::jsonb) || '{"resolution":"auto"}'::jsonb,
    updated_at = now()
WHERE runtime_rule#>>'{upstream,adapter}' = 'zex_video'
  AND (runtime_rule->>'template_key' = 'zex_seedance_2_5'
       OR new_api_model ~ '^seedance-2\.5-(10|15|30)s$')
  AND input_schema#>'{properties,resolution,enum}' = '["480p","720p"]'::jsonb;
