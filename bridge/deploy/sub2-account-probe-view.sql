-- This view is intentionally separate from the desktop reader surface. It is
-- granted only to the root-owned bridge probe role and must never be exposed
-- through the desktop API. The bridge returns only quota and liveness scalars.
CREATE OR REPLACE VIEW public.cpa_desktop_account_probe
WITH (security_barrier = true)
AS
SELECT
    a.id AS account_id,
    a.type AS account_type,
    a.platform,
    a.status,
    a.schedulable,
    a.expires_at,
    a.temp_unschedulable_until,
    CASE WHEN a.platform = 'openai' AND a.type = 'oauth'
        THEN NULLIF(a.credentials->>'access_token', '') END AS access_token,
    CASE WHEN a.platform = 'openai' AND a.type = 'oauth'
        THEN COALESCE(
            NULLIF(a.credentials->>'chatgpt_account_id', ''),
            NULLIF(a.credentials->>'organization_id', '')
        ) END AS chatgpt_account_id,
    CASE WHEN a.platform = 'openai' AND a.type = 'oauth'
        THEN COALESCE(
            NULLIF(a.credentials->>'subscription_expires_at', ''),
            NULLIF(a.credentials->>'subscription_active_until', '')
        ) END AS automatic_expiry_at,
    p.protocol AS proxy_protocol,
    p.host AS proxy_host,
    p.port AS proxy_port,
    p.username AS proxy_username,
    p.password AS proxy_password
FROM public.accounts a
LEFT JOIN public.proxies p ON p.id = a.proxy_id AND p.deleted_at IS NULL
WHERE a.deleted_at IS NULL
  AND a.platform = 'openai'
  AND a.type = 'oauth'
  AND a.status = 'active'
  AND a.schedulable
  AND (a.expires_at IS NULL OR a.expires_at > NOW())
  AND (a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until <= NOW());

REVOKE ALL ON public.cpa_desktop_account_probe FROM PUBLIC;
