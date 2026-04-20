#!/bin/bash
# Export Caddy's internal CA certificate so other services can trust it

set -e

CERT_DEST="/root/.local/share/caddy_root.crt"
CADDY_CONTAINER="tfvision-caddy"
MAX_RETRIES=30
RETRY_COUNT=0

echo "Waiting for Caddy to be ready..."

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if docker exec $CADDY_CONTAINER test -f /root/.local/share/caddy/pki/authorities/local/root.crt 2>/dev/null; then
        echo "✅ Caddy CA certificate found!"
        docker cp $CADDY_CONTAINER:/root/.local/share/caddy/pki/authorities/local/root.crt $CERT_DEST
        chmod 644 $CERT_DEST
        echo "✅ Certificate exported to $CERT_DEST"
        exit 0
    fi
    RETRY_COUNT=$((RETRY_COUNT + 1))
    echo "Attempt $RETRY_COUNT/$MAX_RETRIES: Caddy CA not ready yet, retrying in 1s..."
    sleep 1
done

echo "❌ Failed to extract Caddy CA certificate after $MAX_RETRIES attempts"
exit 1
