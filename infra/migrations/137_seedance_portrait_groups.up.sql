CREATE TABLE IF NOT EXISTS seedance_portrait_groups (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  group_id VARCHAR(128) NOT NULL,
  project_name VARCHAR(128) NOT NULL DEFAULT 'default',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(user_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_seedance_portrait_groups_user
  ON seedance_portrait_groups(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS seedance_portrait_assets (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  group_id VARCHAR(128) NOT NULL,
  asset_id VARCHAR(128) NOT NULL,
  asset_type VARCHAR(16) NOT NULL DEFAULT 'Image',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(user_id, asset_id)
);

CREATE INDEX IF NOT EXISTS idx_seedance_portrait_assets_user
  ON seedance_portrait_assets(user_id, created_at DESC);
