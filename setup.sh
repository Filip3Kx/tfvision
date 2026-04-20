#!/usr/bin/env bash
set -euo pipefail

# ─────────────────────────────────────────────────────────────────────────────
# tfvision setup script
# Bootstraps the stack and installs the Caddy root CA so that terraform (and
# every other tool on your machine) trusts the local HTTPS endpoint without
# any extra environment variables.
# ─────────────────────────────────────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# ── 1. Ensure .env exists ────────────────────────────────────────────────────
if [[ ! -f .env ]]; then
  echo "No .env file found - copying from .env.example"
  cp .env.example .env
  echo
  echo "IMPORTANT:"
  echo "- Edit .env and set TFVISION_DOMAIN to the hostname you want to use."
  echo "- Add that hostname to /etc/hosts pointing at 127.0.0.1."
  echo "- Re-run this script when you are done."
  echo
  exit 0
fi

# Load .env into the shell so we can use variables below.
# shellcheck disable=SC2046
export $(grep -v '^\s*#' .env | grep -v '^\s*$' | xargs)

DOMAIN="${TFVISION_DOMAIN:-tfvision.test}"

echo "==> Using domain: $DOMAIN"

# ── 2. /etc/hosts check ──────────────────────────────────────────────────────
if ! grep -qE "127\.0\.0\.1\s+$DOMAIN" /etc/hosts; then
  echo
  echo "ACTION REQUIRED: add this entry to /etc/hosts"
  echo "127.0.0.1 $DOMAIN"
  echo "Run: echo '127.0.0.1 $DOMAIN' | sudo tee -a /etc/hosts"
  echo
  read -rp "Press Enter once you've added the entry (or Ctrl-C to abort)..."
fi

# ── 3. Start the stack ───────────────────────────────────────────────────────
echo "==> Building and starting containers..."
docker compose up -d --build

# ── 4. Wait for Caddy to generate its root CA ────────────────────────────────
CA_PATH="/data/caddy/pki/authorities/local/root.crt"
echo "==> Waiting for Caddy to generate its root CA..."
for i in $(seq 1 30); do
  if docker exec tfvision-caddy test -f "$CA_PATH" 2>/dev/null; then
    break
  fi
  sleep 2
done

if ! docker exec tfvision-caddy test -f "$CA_PATH" 2>/dev/null; then
  echo "ERROR: Caddy root CA not found after 60 seconds. Is the caddy container running?"
  exit 1
fi

# ── 5. Extract the root CA ───────────────────────────────────────────────────
echo "==> Extracting Caddy root CA..."
docker exec tfvision-caddy cat "$CA_PATH" > "$SCRIPT_DIR/caddy_root.crt"
echo "    Saved to $(pwd)/caddy_root.crt"

# ── 6. Install the CA into the system trust store ────────────────────────────
install_ca_linux() {
  local cert_src="$SCRIPT_DIR/caddy_root.crt"

  # Debian / Ubuntu
  if command -v update-ca-certificates &>/dev/null && [[ -d /usr/local/share/ca-certificates ]]; then
    echo "==> Installing CA (Debian/Ubuntu)..."
    sudo cp "$cert_src" /usr/local/share/ca-certificates/tfvision-caddy.crt
    sudo update-ca-certificates
    return
  fi

  # Fedora / RHEL / CentOS
  if command -v update-ca-trust &>/dev/null && [[ -d /etc/pki/ca-trust/source/anchors ]]; then
    echo "==> Installing CA (Fedora/RHEL)..."
    sudo cp "$cert_src" /etc/pki/ca-trust/source/anchors/tfvision-caddy.crt
    sudo update-ca-trust extract
    return
  fi

  # Arch Linux
  if command -v trust &>/dev/null && [[ -d /etc/ca-certificates/trust-source/anchors ]]; then
    echo "==> Installing CA (Arch)..."
    sudo cp "$cert_src" /etc/ca-certificates/trust-source/anchors/tfvision-caddy.crt
    sudo update-ca-trust
    return
  fi

  echo "WARNING: Could not detect your distro's CA store. Install caddy_root.crt manually."
  echo "         Then run: export SSL_CERT_FILE=$(pwd)/caddy_root.crt"
}

install_ca_linux

# ── 7. Verify ────────────────────────────────────────────────────────────────
echo ""
echo "==> Verifying HTTPS connectivity..."
if curl -sf "https://$DOMAIN/.well-known/terraform.json" >/dev/null; then
  echo "    ✓ https://$DOMAIN is reachable and trusted"
else
  echo "    ✗ Could not reach https://$DOMAIN"
  echo "      Try opening it in your browser or run:"
  echo "      SSL_CERT_FILE=$(pwd)/caddy_root.crt curl https://$DOMAIN/.well-known/terraform.json"
fi

echo ""
echo "tfvision is running"
echo "UI:  https://$DOMAIN"
echo "API: https://$DOMAIN/api/v2/"
echo
echo "Terraform cloud config:"
echo "terraform {"
echo "  cloud {"
echo "    hostname     = \"$DOMAIN\""
echo "    organization = \"<your-org-name>\""
echo "    workspaces { name = \"<workspace-name>\" }"
echo "  }"
echo "}"
echo
echo "No extra SSL environment variables needed."
