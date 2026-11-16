#!/bin/sh
set -eu

cat <<EOF > /usr/share/nginx/html/config.js
window.__APP_CONFIG__ = {
  oidcAuthority: "${OIDC_AUTHORITY:-http://localhost:5556/dex}",
  oidcClientId: "${OIDC_CLIENT_ID:-budget-app-client}",
  apiBaseUrl: "${API_BASE_URL:-}"
};
EOF
