CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT NOT NULL UNIQUE,
  username TEXT UNIQUE,
  password_hash TEXT,
  display_name TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin','user')),
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled','pending')),
  email_verified_at TIMESTAMPTZ,
  quota_bytes BIGINT NOT NULL DEFAULT 10737418240,
  used_bytes BIGINT NOT NULL DEFAULT 0,
  max_file_bytes BIGINT NOT NULL DEFAULT 20971520,
  daily_upload_limit BIGINT NOT NULL DEFAULT 100,
  upload_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE users ADD COLUMN IF NOT EXISTS upload_policy JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE users ADD COLUMN IF NOT EXISTS username TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS users_username_idx ON users(lower(username)) WHERE username IS NOT NULL;

CREATE TABLE IF NOT EXISTS system_settings (
  key TEXT PRIMARY KEY,
  value JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS teams (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  owner_id UUID REFERENCES users(id) ON DELETE CASCADE,
  quota_bytes BIGINT NOT NULL DEFAULT 10737418240,
  used_bytes BIGINT NOT NULL DEFAULT 0,
  plan_code TEXT NOT NULL DEFAULT 'free',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS team_members (
  team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner','admin','member','billing')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (team_id, user_id)
);

CREATE TABLE IF NOT EXISTS team_invitations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  invited_by UUID REFERENCES users(id) ON DELETE SET NULL,
  email TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('admin','member','billing')),
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  accepted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS team_invitations_team_idx ON team_invitations(team_id, created_at DESC);
ALTER TABLE team_invitations ADD COLUMN IF NOT EXISTS invited_by UUID REFERENCES users(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS plans (
  code TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  price_cents INTEGER NOT NULL DEFAULT 0,
  quota_bytes BIGINT NOT NULL DEFAULT 10737418240,
  bandwidth_bytes BIGINT NOT NULL DEFAULT 0,
  member_limit INTEGER NOT NULL DEFAULT 1,
  features JSONB NOT NULL DEFAULT '{}'::jsonb,
  active BOOLEAN NOT NULL DEFAULT true
);
ALTER TABLE plans ADD COLUMN IF NOT EXISTS provider_price_id TEXT;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS billing_interval TEXT NOT NULL DEFAULT 'month';

CREATE TABLE IF NOT EXISTS subscriptions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  plan_code TEXT NOT NULL REFERENCES plans(code),
  status TEXT NOT NULL DEFAULT 'active',
  provider TEXT,
  provider_subscription_id TEXT,
  current_period_end TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS subscriptions_provider_idx ON subscriptions(provider, provider_subscription_id) WHERE provider_subscription_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS billing_events (
  provider TEXT NOT NULL,
  event_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(provider, event_id)
);

CREATE TABLE IF NOT EXISTS images (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  team_id UUID REFERENCES teams(id) ON DELETE SET NULL,
  object_key TEXT NOT NULL,
  storage_backend TEXT NOT NULL DEFAULT 'local',
  original_name TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  extension TEXT NOT NULL,
  size_bytes BIGINT NOT NULL,
  width INTEGER,
  height INTEGER,
  duration_seconds NUMERIC,
  visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private','public','link')),
  status TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('processing','ready','failed','deleted')),
  expires_at TIMESTAMPTZ,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS images_owner_created_idx ON images(owner_id, created_at DESC);
CREATE INDEX IF NOT EXISTS images_hash_idx ON images(content_hash);
CREATE INDEX IF NOT EXISTS images_team_created_idx ON images(team_id, created_at DESC);
ALTER TABLE images ADD COLUMN IF NOT EXISTS thumbnail_object_key TEXT;
ALTER TABLE images ADD COLUMN IF NOT EXISTS processing_error TEXT;
ALTER TABLE images ADD COLUMN IF NOT EXISTS deleted_by UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE images ADD COLUMN IF NOT EXISTS link_token_hash TEXT;
ALTER TABLE images ALTER COLUMN owner_id DROP NOT NULL;
ALTER TABLE images ADD COLUMN IF NOT EXISTS guest_expires_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS images_link_token_idx ON images(link_token_hash) WHERE link_token_hash IS NOT NULL;

CREATE TABLE IF NOT EXISTS short_links (
  code TEXT PRIMARY KEY,
  image_id UUID NOT NULL REFERENCES images(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS short_links_image_idx ON short_links(image_id);

CREATE TABLE IF NOT EXISTS media_jobs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  image_id UUID NOT NULL REFERENCES images(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('video_inspect','video_thumbnail','transcode')),
  status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','succeeded','failed')),
  attempts INTEGER NOT NULL DEFAULT 0,
  available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  locked_at TIMESTAMPTZ,
  locked_by TEXT,
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(image_id, kind)
);
CREATE INDEX IF NOT EXISTS media_jobs_ready_idx ON media_jobs(status, available_at);

CREATE TABLE IF NOT EXISTS custom_domains (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id UUID REFERENCES users(id) ON DELETE CASCADE,
  team_id UUID REFERENCES teams(id) ON DELETE CASCADE,
  domain TEXT NOT NULL UNIQUE,
  verification_token TEXT NOT NULL,
  verified_at TIMESTAMPTZ,
  tls_status TEXT NOT NULL DEFAULT 'pending',
  is_default BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE custom_domains ADD COLUMN IF NOT EXISTS verification_method TEXT NOT NULL DEFAULT 'dns_txt';
ALTER TABLE custom_domains ADD COLUMN IF NOT EXISTS last_checked_at TIMESTAMPTZ;
ALTER TABLE custom_domains ADD COLUMN IF NOT EXISTS certificate_expires_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS oidc_providers (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  issuer_url TEXT NOT NULL,
  client_id TEXT NOT NULL,
  client_secret_encrypted TEXT NOT NULL,
  scopes TEXT[] NOT NULL DEFAULT ARRAY['openid','profile','email'],
  enabled BOOLEAN NOT NULL DEFAULT false,
  auto_create_users BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS oidc_subjects (
  provider_id UUID NOT NULL REFERENCES oidc_providers(id) ON DELETE CASCADE,
  subject TEXT NOT NULL,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (provider_id, subject)
);

CREATE TABLE IF NOT EXISTS email_tokens (
  token_hash TEXT PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('verify','reset')),
  expires_at TIMESTAMPTZ NOT NULL,
  used_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id BIGSERIAL PRIMARY KEY,
  actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
  action TEXT NOT NULL,
  target_type TEXT,
  target_id TEXT,
  request_id TEXT,
  ip_hash TEXT,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO plans (code, name, price_cents, quota_bytes, member_limit, features)
VALUES ('free', 'Free', 0, 10737418240, 1, '{"remote_import":false,"custom_domain":false}'::jsonb)
ON CONFLICT (code) DO NOTHING;
INSERT INTO plans (code, name, price_cents, quota_bytes, bandwidth_bytes, member_limit, features)
VALUES ('pro', 'Pro', 1200, 107374182400, 1073741824000, 5, '{"remote_import":true,"custom_domain":true,"video":true}'::jsonb),
       ('team', 'Team', 3900, 1073741824000, 10737418240000, 25, '{"remote_import":true,"custom_domain":true,"video":true,"priority_support":true}'::jsonb)
ON CONFLICT (code) DO NOTHING;
INSERT INTO system_settings (key, value) VALUES
 ('site', '{"default_language":"en-US"}'::jsonb),
 ('registration', '{"enabled":true,"require_email_verification":false,"password_reset_enabled":true}'::jsonb),
 ('upload', '{"max_file_bytes":20971520,"daily_upload_limit":100,"allowed_mime_types":["image/jpeg","image/png","image/gif","image/webp","image/avif","image/svg+xml","video/mp4","video/webm"],"allow_svg":true,"allow_video":true,"allow_remote_url":true,"anonymous_enabled":false,"guest_daily_upload_limit":10,"guest_daily_upload_bytes":0,"guest_retention_days":7,"short_links_enabled":true,"short_code_length":8,"naming_mode":"sha256","directory_rule":"hash2","path_template":"","random_length":12,"default_visibility":"private"}'::jsonb),
 ('email', '{"smtp_host":"","smtp_port":587,"smtp_username":"","smtp_security":"starttls","from_name":"ImageHub","from_address":""}'::jsonb),
 ('storage', '{"backend":"local","telegram_chat_id":"","cdn_base_url":""}'::jsonb),
 ('oidc', '{"enabled":false,"issuer_url":"","client_id":"","scopes":"openid profile email","auto_create_users":true}'::jsonb)
ON CONFLICT (key) DO NOTHING;
