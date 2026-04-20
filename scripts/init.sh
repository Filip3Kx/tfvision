#!/bin/bash
# Initialization script to ensure all prerequisites are ready

set -e

echo "🚀 TFVision Startup"
echo "===================="

# Wait for database
echo "Waiting for database..."
until PGPASSWORD=tfvision pg_isready -h db -U tfvision -d tfvision 2>/dev/null; do
    echo "Database not ready, retrying..."
    sleep 2
done
echo "✅ Database is ready"

# Wait for Caddy and extract certificate
echo "Waiting for Caddy CA certificate..."
MAX_RETRIES=30
RETRY_COUNT=0
while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if docker exec tfvision-caddy test -f /root/.local/share/caddy/pki/authorities/local/root.crt 2>/dev/null; then
        docker cp tfvision-caddy:/root/.local/share/caddy/pki/authorities/local/root.crt /root/.local/share/caddy_root.crt 2>/dev/null || true
        chmod 644 /root/.local/share/caddy_root.crt 2>/dev/null || true
        echo "✅ Caddy CA certificate obtained"
        break
    fi
    RETRY_COUNT=$((RETRY_COUNT + 1))
    echo "Attempt $RETRY_COUNT/$MAX_RETRIES: Caddy not ready yet..."
    sleep 1
done

echo "✅ All prerequisites ready"
