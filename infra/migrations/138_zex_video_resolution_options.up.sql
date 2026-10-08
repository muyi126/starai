-- Repair only the original free-text Zex template field. Explicitly configured
-- enums remain authoritative, including resolutions added by administrators.
WITH legacy AS (
  SELECT id,
    CASE WHEN new_api_model IN ('minimax-h3', 'minimax-h3-max')
           OR runtime_rule->>'template_key' IN ('zex_minimax_h3', 'zex_minimax_h3_max')
      THEN '["480p","768p"]'::jsonb ELSE '["480p","720p"]'::jsonb END AS resolutions
  FROM models
  WHERE runtime_rule#>>'{upstream,adapter}' = 'zex_video'
    AND input_schema#>>'{properties,resolution,type}' = 'string'
    AND input_schema#>>'{properties,resolution,title}' = '分辨率（可选）'
    AND COALESCE(input_schema#>>'{properties,resolution,default}', '') = ''
    AND NOT (input_schema#>'{properties,resolution}' ? 'enum')
), configured AS (
  SELECT m.id, l.resolutions,
    CASE WHEN l.resolutions ? (m.default_params->>'resolution')
      THEN m.default_params->>'resolution' ELSE '480p' END AS default_resolution
  FROM models m JOIN legacy l ON l.id = m.id
)
UPDATE models m
SET input_schema = jsonb_set(m.input_schema, '{properties,resolution}',
      m.input_schema#>'{properties,resolution}' || jsonb_build_object(
        'title', '分辨率', 'enum', c.resolutions, 'default', c.default_resolution,
        'x-widget', 'option_menu', 'x-icon', '4k', 'x-order', 3)),
    default_params = COALESCE(m.default_params, '{}'::jsonb)
      || jsonb_build_object('resolution', c.default_resolution),
    updated_at = now()
FROM configured c WHERE m.id = c.id;
