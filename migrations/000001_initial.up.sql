CREATE TABLE users (
    id bigserial PRIMARY KEY, username text NOT NULL UNIQUE, display_name text NOT NULL,
    password_hash text NOT NULL, enabled boolean NOT NULL DEFAULT true,
    must_change_password boolean NOT NULL DEFAULT true, preferences jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE roles (
    id bigserial PRIMARY KEY, name text NOT NULL UNIQUE,
    permissions jsonb NOT NULL DEFAULT '[]', scopes jsonb NOT NULL DEFAULT '[]',
    administrator boolean NOT NULL DEFAULT false
);
CREATE UNIQUE INDEX one_administrator_role ON roles (administrator) WHERE administrator;
CREATE TABLE user_roles (user_id bigint REFERENCES users(id) ON DELETE CASCADE, role_id bigint REFERENCES roles(id) ON DELETE CASCADE, PRIMARY KEY(user_id,role_id));
CREATE TABLE sessions (
    id text PRIMARY KEY, user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), last_seen timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user ON sessions(user_id);
CREATE TABLE cloud_accounts (
    id bigserial PRIMARY KEY, name text NOT NULL UNIQUE, provider text NOT NULL DEFAULT 'tencent',
    secret_id text NOT NULL, secret_key text NOT NULL, regions jsonb NOT NULL DEFAULT '[]',
    enabled boolean NOT NULL DEFAULT true, last_sync timestamptz, sync_error text NOT NULL DEFAULT ''
);
CREATE TABLE resources (
    id bigserial PRIMARY KEY, account_id bigint NOT NULL REFERENCES cloud_accounts(id),
    provider text NOT NULL DEFAULT 'tencent', kind text NOT NULL, region text NOT NULL,
    instance_id text NOT NULL, name text NOT NULL, public_ip text NOT NULL DEFAULT '', private_ip text NOT NULL DEFAULT '',
    spec text NOT NULL DEFAULT '', cpu integer NOT NULL DEFAULT 0, memory integer NOT NULL DEFAULT 0,
    disk integer NOT NULL DEFAULT 0, state text NOT NULL, tags jsonb NOT NULL DEFAULT '{}',
    metrics jsonb NOT NULL DEFAULT '{}', synced_at timestamptz NOT NULL DEFAULT now(),
    tags_error text NOT NULL DEFAULT '', UNIQUE(account_id,kind,region,instance_id)
);
CREATE TABLE audits (
    id bigserial PRIMARY KEY, user_id bigint, actor text NOT NULL, source_ip text NOT NULL DEFAULT '',
    action text NOT NULL, target text NOT NULL DEFAULT '', result text NOT NULL, message text NOT NULL DEFAULT '',
    request_id text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audits_time ON audits(created_at DESC);
CREATE TABLE tasks (
    id bigserial PRIMARY KEY, resource_id bigint REFERENCES resources(id), account_id bigint REFERENCES cloud_accounts(id),
    audit_id bigint REFERENCES audits(id) ON DELETE SET NULL, action text NOT NULL, status text NOT NULL DEFAULT 'queued',
    request_id text NOT NULL DEFAULT '', error text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(), next_run timestamptz NOT NULL DEFAULT now(), lease_until timestamptz,
    baseline_state text NOT NULL DEFAULT '', observed_transition boolean NOT NULL DEFAULT false
);
CREATE UNIQUE INDEX active_resource_task ON tasks(resource_id) WHERE status IN ('queued','submitting','waiting','unknown');
CREATE UNIQUE INDEX active_sync_task ON tasks(account_id) WHERE action='sync' AND status IN ('queued','submitting','waiting','unknown');
CREATE INDEX tasks_ready ON tasks(next_run);
CREATE TABLE settings (
    id integer PRIMARY KEY CHECK (id=1), refresh_seconds integer NOT NULL DEFAULT 60,
    idle_minutes integer NOT NULL DEFAULT 30, max_hours integer NOT NULL DEFAULT 8,
    retention_days integer NOT NULL DEFAULT 180, metrics jsonb NOT NULL DEFAULT '["cpu","memory","disk","network_in","network_out"]'
);
INSERT INTO settings(id) VALUES(1);
CREATE TABLE oauth_providers (
    id bigserial PRIMARY KEY, name text NOT NULL UNIQUE, client_id text NOT NULL, client_secret text NOT NULL,
    auth_url text NOT NULL, token_url text NOT NULL, user_url text NOT NULL, redirect_url text NOT NULL,
    scopes text NOT NULL DEFAULT '', subject_field text NOT NULL DEFAULT 'sub', enabled boolean NOT NULL DEFAULT true
);
CREATE TABLE identities (
    id bigserial PRIMARY KEY, provider_id bigint NOT NULL REFERENCES oauth_providers(id),
    subject text NOT NULL, user_id bigint NOT NULL REFERENCES users(id), UNIQUE(provider_id,subject)
);
CREATE TABLE oauth_states (
    id text PRIMARY KEY, provider_id bigint NOT NULL REFERENCES oauth_providers(id),
    user_id bigint REFERENCES users(id), session_id text NOT NULL DEFAULT '', verifier text NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE TABLE login_attempts (key text PRIMARY KEY, attempts integer NOT NULL DEFAULT 0, expires_at timestamptz NOT NULL);
CREATE TABLE key_checks (id integer PRIMARY KEY CHECK(id=1), value text NOT NULL);
