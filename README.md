# tfvision

A self-hosted Terraform state platform with a web UI.

tfvision is state-first and local-execution friendly:
- Terraform still runs on your machine or in CI.
- State versions are stored in PostgreSQL.
- A companion `tfvision` CLI can push finalized Terraform logs and synced state metadata into the UI.
- UI focuses on state history, version diffs, resource relationships, and captured runs.

## Prerequisites

- Docker + Docker Compose v2
- Git
- Linux host
- terraform CLI (1.14.x recommended)
- Go 1.25+ if you want to build the companion `tfvision` binary locally
- curl, sudo

## 1. Clone

```bash
git clone https://github.com/your-org/tfvision.git
cd tfvision
```

## 2. Configure hostname

Add your tfvision host to /etc/hosts:

```bash
echo '127.0.0.1 tfvision.test' | sudo tee -a /etc/hosts
```

## 3. Configure env

```bash
cp .env.example .env
```

Set the domain:

```dotenv
TFVISION_DOMAIN=tfvision.test
```

## 4. Bootstrap

```bash
./setup.sh
```

The script:
- Starts containers (caddy, backend, db, frontend)
- Extracts Caddy root CA
- Installs CA in your trust store
- Verifies HTTPS

If you change backend or frontend code later, rebuild the live stack explicitly:

```bash
docker compose up -d --build backend frontend
```

## 5. Open UI

- https://tfvision.test

Workspace pages currently expose:
- State History
- State Diff
- Runs
- Resource Canvas

## 6. Terraform cloud block

Use cloud mode with self-hosted hostname:

```hcl
terraform {
  cloud {
    hostname     = "tfvision.test"
    organization = "my-org"
    workspaces {
      name = "my-workspace"
    }
  }
}
```

Set token env var (any non-empty value is accepted):

```bash
export TF_TOKEN_tfvision_test=dummy-token
```

## 7. Run Terraform locally

```bash
terraform init
terraform plan
terraform apply
```

Terraform executes locally. tfvision stores state snapshots and serves them through its API.

## 8. Build the companion CLI

The separate `tfvision` binary is used for CI/CD-oriented metadata sync. It does not replace Terraform and does not proxy Terraform flags.

Build it from the backend module:

```bash
cd backend
go build -o tfvision ./cmd/tfvision
```

You can then run it as `./tfvision ...` from the `backend` directory, or move it somewhere on your `PATH`.

Available commands:
- `sync-state`
- `pull-tf-logs`

## 9. End-to-end workflow

The intended flow is:

1. Bring up tfvision server.
2. Run normal Terraform commands.
3. Capture logs from `plan` or `apply`.
4. Push those logs into tfvision as a run.
5. Sync the resulting state and provider versions into tfvision.
6. Inspect the outcome in the UI.

### Example environment

```bash
export TF_TOKEN_tfvision_test=dummy-token
export TFVISION_HOST=https://localhost
export TFVISION_ORGANIZATION=my-org
export TFVISION_WORKSPACE=my-workspace
```

### Step 1: initialize Terraform normally

```bash
terraform init
```

`init` is still just Terraform. tfvision does not wrap it.

### Step 2: run plan and capture its output

```bash
terraform plan | tee plan.log
```

If the plan succeeds and you want it visible in the UI under `Runs`:

```bash
./backend/tfvision pull-tf-logs \
  --host "$TFVISION_HOST" \
  --organization "$TFVISION_ORGANIZATION" \
  --workspace "$TFVISION_WORKSPACE" \
  --command plan \
  --status planned \
  --message "Plan completed successfully" \
  --log-file plan.log
```

You can also pipe logs through stdin instead of using a file:

```bash
terraform plan 2>&1 | tee /tmp/plan.log
cat /tmp/plan.log | ./backend/tfvision pull-tf-logs \
  --host "$TFVISION_HOST" \
  --organization "$TFVISION_ORGANIZATION" \
  --workspace "$TFVISION_WORKSPACE" \
  --command plan \
  --status planned \
  --message "Plan completed successfully" \
  --stdin
```

### Step 3: run apply and capture its output

```bash
terraform apply | tee apply.log
```

If apply succeeds, upload the run log:

```bash
./backend/tfvision pull-tf-logs \
  --host "$TFVISION_HOST" \
  --organization "$TFVISION_ORGANIZATION" \
  --workspace "$TFVISION_WORKSPACE" \
  --command apply \
  --status applied \
  --message "Apply completed successfully" \
  --log-file apply.log
```

### Step 4: sync provider versions onto the DB-backed state

After `apply`, sync provider versions from `.terraform.lock.hcl` onto the latest state already stored in tfvision:

```bash
./backend/tfvision sync-state \
  --host "$TFVISION_HOST" \
  --organization "$TFVISION_ORGANIZATION" \
  --workspace "$TFVISION_WORKSPACE" \
  --lock-file .terraform.lock.hcl
```

This does two things:
- keeps the existing latest state version in tfvision as the source of truth
- enriches provider versions from `.terraform.lock.hcl` when present

Optional: if you need to manually import a local state file, pass `--state-file`.

If you need to override metadata manually:

```bash
./backend/tfvision sync-state \
  --host "$TFVISION_HOST" \
  --organization "$TFVISION_ORGANIZATION" \
  --workspace "$TFVISION_WORKSPACE" \
  --lock-file .terraform.lock.hcl \
  --serial 12 \
  --lineage your-lineage-id
```

Manual local-state import example:

```bash
./backend/tfvision sync-state \
  --host "$TFVISION_HOST" \
  --organization "$TFVISION_ORGANIZATION" \
  --workspace "$TFVISION_WORKSPACE" \
  --state-file terraform.tfstate \
  --lock-file .terraform.lock.hcl
```

### Step 5: report failures

If `plan` or `apply` fails, you can still store the error in tfvision even if no state was produced:

```bash
terraform apply 2>&1 | tee apply-error.log

./backend/tfvision pull-tf-logs \
  --host "$TFVISION_HOST" \
  --organization "$TFVISION_ORGANIZATION" \
  --workspace "$TFVISION_WORKSPACE" \
  --command apply \
  --status error \
  --message "Apply failed in CI" \
  --log-file apply-error.log
```

## 10. What appears in the UI

- `State History`
  - uploaded state versions
  - summary information for a selected state version
  - raw state JSON for the selected version
- `State Diff`
  - resource add/change/remove summary between two versions
  - git-style JSON diff rendering
- `Runs`
  - one entry per uploaded command log
  - statuses: `planned`, `applied`, `error`
  - command, message, timestamps, and full uploaded log body
- `Resource Canvas`
  - current-state resource graph
  - module-aware resource addresses
  - provider source and version when state was synced through `tfvision sync-state`

## UI Features

- State History: Timeline of uploaded state versions
- State Diff: Compare two state versions (added/changed/removed resources + raw diff payload)
- Runs: Stored command output from `tfvision pull-tf-logs`
- Resource Canvas: Graph-style view with clickable resource details and dependencies

## Services

- caddy: TLS + reverse proxy + static frontend serving
- backend: Go API for org/workspace/state/diff/resources/runs
- db: PostgreSQL
- frontend: React app build container

## Troubleshooting

- 502 during startup:
  - Wait for backend to finish booting and retry terraform init.
- CLI changes not visible in browser:
  - Rebuild services with `docker compose up -d --build backend frontend`.
- Lock errors:
  - tfvision uses workspace lock/unlock endpoints for local operations.
- TLS errors:
  - Re-run ./setup.sh to reinstall CA trust.
- Provider versions still show as `unknown`:
  - sync state through `tfvision sync-state --lock-file .terraform.lock.hcl`.
- Runs page is empty:
  - `terraform plan/apply` alone does not upload logs; use `tfvision pull-tf-logs` after the command completes.

## Architecture

```text
 Terraform CLI / tfvision CLI / Browser
          |
        HTTPS
          |
        Caddy
      /       \
 Backend API   Frontend static
      |
   PostgreSQL
```
