ALTER TABLE provider_identities ADD COLUMN encrypted_refresh_token bytea;
ALTER TABLE provider_identities ADD COLUMN token_client_id text;
