CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL UNIQUE CHECK (email = lower(email)),
    name text NOT NULL DEFAULT '',
    password_hash text,
    role text NOT NULL DEFAULT 'reader' CHECK (role IN ('reader','admin','owner')),
    verified boolean NOT NULL DEFAULT false,
    suspended boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE auth_sessions (
    token_hash bytea PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX auth_sessions_user_idx ON auth_sessions(user_id);
CREATE TABLE provider_identities (
    provider text NOT NULL CHECK (provider IN ('google','apple')),
    subject text NOT NULL,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(provider, subject),
    UNIQUE(user_id, provider)
);
CREATE TABLE auth_tokens (
    token_hash bytea PRIMARY KEY,
    purpose text NOT NULL CHECK (purpose IN ('verify','reset','invite')),
    user_id uuid REFERENCES users(id) ON DELETE CASCADE,
    email text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX auth_tokens_account_idx ON auth_tokens(email,purpose);
CREATE TABLE auth_challenges (
    nonce_hash bytea PRIMARY KEY,
    user_id uuid REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);
CREATE TABLE auth_rate_limits (
    bucket bytea PRIMARY KEY,
    hits integer NOT NULL,
    reset_at timestamptz NOT NULL
);
CREATE TABLE mail_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient text NOT NULL,
    purpose text NOT NULL CHECK (purpose IN ('verify','reset','invite')),
    encrypted_body bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    claimed_until timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sent','failed')),
    sent_at timestamptz
);
CREATE INDEX mail_outbox_pending_idx ON mail_outbox(next_attempt_at) WHERE status='pending';
CREATE INDEX mail_outbox_quota_idx ON mail_outbox(created_at);
CREATE INDEX mail_outbox_recipient_idx ON mail_outbox(recipient,created_at);
CREATE TABLE mail_usage (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    recipient_hash bytea NOT NULL,
    stage text NOT NULL CHECK (stage IN ('queued','attempted')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mail_usage_quota_idx ON mail_usage(stage,created_at);
CREATE TABLE mail_alerts (
    kind text PRIMARY KEY,
    message text NOT NULL,
    occurrences bigint NOT NULL DEFAULT 1,
    last_seen_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE security_audit (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id uuid REFERENCES users(id) ON DELETE SET NULL,
    target_id uuid REFERENCES users(id) ON DELETE SET NULL,
    action text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX security_audit_created_idx ON security_audit(created_at);
ALTER TABLE favorites ADD CONSTRAINT favorites_user_fk FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE;
