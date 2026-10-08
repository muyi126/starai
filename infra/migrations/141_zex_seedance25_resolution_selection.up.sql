-- Restore user-selectable resolutions after migration 140. Only the old auto
-- preset is replaced; custom menus, routing, media modes and prices are kept.
UPDATE models
SET input_schema = jsonb_set(input_schema, '{properties,resolution}',
      ((input_schema#>'{properties,resolution}') - 'enumLabels' - 'x-omit-auto') ||
      '{"enum":["480p","720p"],"default":"480p"}'::jsonb),
    default_params = COALESCE(default_params, '{}'::jsonb) || '{"resolution":"480p"}'::jsonb,
    updated_at = now()
WHERE runtime_rule#>>'{upstream,adapter}' = 'zex_video'
  AND (runtime_rule->>'template_key' = 'zex_seedance_2_5'
       OR new_api_model ~ '^seedance-2\.5-(10|15|30)s$')
  AND input_schema#>'{properties,resolution,enum}' = '["auto"]'::jsonb;
