-- Run as the Sub2 database owner. The reader receives this view only, never
-- the underlying credentials or extra JSON documents.
CREATE OR REPLACE VIEW public.cpa_desktop_account_usage
WITH (security_barrier = true)
AS
SELECT
    id AS account_id,
    NULLIF(credentials->>'plan_type', '') AS plan_type,
    CASE WHEN extra->>'codex_5h_used_percent' ~ '^[0-9]+([.][0-9]+)?$'
        THEN (extra->>'codex_5h_used_percent')::double precision END AS five_hour_used_percent,
    NULLIF(extra->>'codex_5h_reset_at', '') AS five_hour_reset_at,
    CASE
        WHEN extra->>'codex_7d_used_percent' ~ '^[0-9]+([.][0-9]+)?$'
            THEN (extra->>'codex_7d_used_percent')::double precision
        WHEN extra->>'quota_weekly_limit' ~ '^[0-9]+([.][0-9]+)?$'
             AND (extra->>'quota_weekly_limit')::double precision > 0
             AND extra->>'quota_weekly_used' ~ '^[0-9]+([.][0-9]+)?$'
            THEN (extra->>'quota_weekly_used')::double precision
                 / (extra->>'quota_weekly_limit')::double precision * 100
    END AS weekly_used_percent,
    CASE
        WHEN NULLIF(extra->>'codex_7d_reset_at', '') IS NOT NULL
            THEN extra->>'codex_7d_reset_at'
        WHEN extra->>'quota_weekly_start' ~ '^[0-9]{10}([.][0-9]+)?$'
            THEN (to_timestamp((extra->>'quota_weekly_start')::double precision) + interval '7 days')::text
        WHEN extra->>'quota_weekly_start' ~ '^[0-9]{13}$'
            THEN (to_timestamp((extra->>'quota_weekly_start')::double precision / 1000) + interval '7 days')::text
        WHEN extra->>'quota_weekly_start' ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}'
            THEN ((extra->>'quota_weekly_start')::timestamptz + interval '7 days')::text
    END AS weekly_reset_at,
    CASE WHEN extra->>'quota_weekly_limit' ~ '^[0-9]+([.][0-9]+)?$'
        THEN (extra->>'quota_weekly_limit')::double precision END AS weekly_limit,
    CASE WHEN extra->>'quota_weekly_used' ~ '^[0-9]+([.][0-9]+)?$'
        THEN (extra->>'quota_weekly_used')::double precision END AS weekly_usage,
    COALESCE(
        NULLIF(extra->>'codex_usage_updated_at', ''),
        NULLIF(extra#>>'{upstream_billing_probe,updated_at}', '')
    ) AS usage_updated_at,
    COALESCE(
        NULLIF(extra->>'rate_limit_reset_at', ''),
        NULLIF(extra#>>'{upstream_billing_probe,rate_limit_reset_at}', ''),
        NULLIF(extra#>>'{upstream_billing_probe,reset_at}', '')
    ) AS rate_limit_reset_at,
    NULLIF(extra->>'ops_health', '') AS ops_health
FROM public.accounts;

REVOKE ALL ON public.cpa_desktop_account_usage FROM PUBLIC;
