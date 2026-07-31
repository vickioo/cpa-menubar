#!/usr/bin/env bash
set -euo pipefail

repo_dir="${1:?usage: deploy-sub2-docker.sh REPOSITORY_DIR}"
deploy_dir="/opt/cpa-sub2-bridge"
container_name="cpa-sub2-bridge"
network_name="sub2api_sub2api-network"
postgres_container="sub2api-postgres"
listen_port="18330"

sudo install -d -m 0700 "$deploy_dir"
if sudo test -f "$deploy_dir/bridge.env"; then
  sudo cp -a "$deploy_dir/bridge.env" "$deploy_dir/bridge.env.backup.$(date +%Y%m%d-%H%M%S)"
fi

sudo docker run --rm \
  -e GOPROXY=https://goproxy.cn,direct \
  -e CGO_ENABLED=0 \
  -v "$repo_dir/bridge:/src:ro" \
  -v "$deploy_dir:/out" \
  -w /src \
  golang:1.25-bookworm \
  sh -c 'go build -trimpath -ldflags="-s -w" -o /out/cpa-desktop-bridge ./cmd/cpa-desktop-bridge'
sudo chmod 0755 "$deploy_dir/cpa-desktop-bridge"

db_user="$(sudo docker exec "$postgres_container" printenv POSTGRES_USER)"
db_name="$(sudo docker exec "$postgres_container" printenv POSTGRES_DB)"
reader_password="$(openssl rand -hex 24)"
desktop_token="$(openssl rand -hex 32)"

role_exists="$(sudo docker exec "$postgres_container" psql -U "$db_user" -d "$db_name" -Atc "SELECT 1 FROM pg_roles WHERE rolname='cpa_desktop_reader'")"
if [[ "$role_exists" == "1" ]]; then
  sudo docker exec "$postgres_container" psql -U "$db_user" -d "$db_name" -v ON_ERROR_STOP=1 \
    -c "ALTER ROLE cpa_desktop_reader LOGIN PASSWORD '$reader_password'"
else
  sudo docker exec "$postgres_container" psql -U "$db_user" -d "$db_name" -v ON_ERROR_STOP=1 \
    -c "CREATE ROLE cpa_desktop_reader LOGIN PASSWORD '$reader_password'"
fi

sudo docker exec "$postgres_container" psql -U "$db_user" -d "$db_name" -v ON_ERROR_STOP=1 \
  -c "GRANT CONNECT ON DATABASE \"$db_name\" TO cpa_desktop_reader" \
  -c "GRANT USAGE ON SCHEMA public TO cpa_desktop_reader" \
  -c "GRANT SELECT (id,name,platform,type,status,schedulable,priority,expires_at,last_used_at,temp_unschedulable_until,deleted_at,notes) ON public.accounts TO cpa_desktop_reader" \
  -c "GRANT SELECT (account_id,group_id) ON public.account_groups TO cpa_desktop_reader" \
  -c "GRANT SELECT (id,name) ON public.groups TO cpa_desktop_reader" \
  -c "GRANT SELECT (account_id,created_at) ON public.usage_logs TO cpa_desktop_reader" \
  -c "GRANT SELECT (account_id,created_at) ON public.ops_error_logs TO cpa_desktop_reader" \
  -c "ALTER ROLE cpa_desktop_reader SET default_transaction_read_only = on" \
  -c "ALTER ROLE cpa_desktop_reader SET statement_timeout = '5s'"

env_tmp="$(mktemp)"
trap 'rm -f "$env_tmp"' EXIT
umask 077
{
  echo "CPA_DESKTOP_LISTEN=0.0.0.0:8330"
  echo "CPA_DESKTOP_TOKEN=$desktop_token"
  echo "CPA_ACCOUNT_SOURCE=sub2"
  echo "SUB2_DATABASE_URL=postgres://cpa_desktop_reader:$reader_password@$postgres_container:5432/$db_name?sslmode=disable"
  echo "SUB2_FOCUS_PRIORITY=4"
  echo "SUB2_FOCUS_GROUP_IDS=2,8,9,12,13"
} > "$env_tmp"
sudo install -m 0600 "$env_tmp" "$deploy_dir/bridge.env"

printf '%s\n' \
  'FROM scratch' \
  'COPY cpa-desktop-bridge /cpa-desktop-bridge' \
  'ENTRYPOINT ["/cpa-desktop-bridge"]' \
  | sudo docker build -t cpa-sub2-bridge:local -f - "$deploy_dir"

if sudo docker container inspect "$container_name" >/dev/null 2>&1; then
  sudo docker rm -f "$container_name" >/dev/null
fi
sudo docker run -d \
  --name "$container_name" \
  --restart unless-stopped \
  --network "$network_name" \
  --env-file "$deploy_dir/bridge.env" \
  -p "127.0.0.1:$listen_port:8330" \
  cpa-sub2-bridge:local >/dev/null

for _ in {1..20}; do
  if curl -fsS -H "Authorization: Bearer $desktop_token" "http://127.0.0.1:$listen_port/desktop/v1/summary" >/dev/null; then
    echo "Sub2 bridge is healthy on 127.0.0.1:$listen_port"
    exit 0
  fi
  sleep 1
done

sudo docker logs --tail 30 "$container_name"
exit 1
