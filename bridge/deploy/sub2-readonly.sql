-- Run as the Sub2 database owner. Replace the password before execution and
-- keep the resulting connection string outside Git. Apply
-- sub2-account-usage-view.sql before granting the view below.
CREATE ROLE cpa_desktop_reader LOGIN PASSWORD 'REPLACE_WITH_A_LONG_RANDOM_PASSWORD';

GRANT CONNECT ON DATABASE sub2 TO cpa_desktop_reader;
GRANT USAGE ON SCHEMA public TO cpa_desktop_reader;

GRANT SELECT (
    id, name, platform, type, status, schedulable, expires_at,
    last_used_at, temp_unschedulable_until, deleted_at
) ON TABLE public.accounts TO cpa_desktop_reader;
GRANT SELECT (account_id, group_id) ON TABLE public.account_groups TO cpa_desktop_reader;
GRANT SELECT (id, name) ON TABLE public.groups TO cpa_desktop_reader;
GRANT SELECT (account_id, created_at) ON TABLE public.usage_logs TO cpa_desktop_reader;
GRANT SELECT (account_id, created_at) ON TABLE public.ops_error_logs TO cpa_desktop_reader;
GRANT SELECT ON TABLE public.cpa_desktop_account_usage TO cpa_desktop_reader;

ALTER ROLE cpa_desktop_reader SET default_transaction_read_only = on;
ALTER ROLE cpa_desktop_reader SET statement_timeout = '5s';

-- Intentionally do not grant table-level SELECT on accounts. The quota view
-- exposes only named scalar fields; credentials, extra, tokens, passwords,
-- and provider secrets remain denied.

-- The live probe role is a second trust boundary. It can read only the narrow
-- probe view and remains unable to query accounts, credentials, or proxies
-- directly. Keep its connection string in the root-owned bridge.env file.
CREATE ROLE cpa_desktop_probe LOGIN PASSWORD 'REPLACE_WITH_ANOTHER_LONG_RANDOM_PASSWORD';
GRANT CONNECT ON DATABASE sub2 TO cpa_desktop_probe;
GRANT USAGE ON SCHEMA public TO cpa_desktop_probe;
GRANT SELECT ON TABLE public.cpa_desktop_account_probe TO cpa_desktop_probe;
ALTER ROLE cpa_desktop_probe SET default_transaction_read_only = on;
ALTER ROLE cpa_desktop_probe SET statement_timeout = '25s';
