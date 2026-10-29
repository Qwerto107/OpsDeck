CREATE TABLE oauth_bindings (
    id text PRIMARY KEY,
    provider_id bigint NOT NULL REFERENCES oauth_providers(id),
    subject text NOT NULL,
    csrf text NOT NULL,
    expires_at timestamptz NOT NULL
);
