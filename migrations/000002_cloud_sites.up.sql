ALTER TABLE cloud_accounts ADD COLUMN site text NOT NULL DEFAULT 'china';
ALTER TABLE cloud_accounts ADD CONSTRAINT cloud_accounts_site CHECK (site IN ('china', 'international'));
