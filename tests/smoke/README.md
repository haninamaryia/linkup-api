# Smoke tests (health contract)

Contract: **`docs/smoke-contracts.yaml`** — only `GET /health` → 200, `{"status":"ok"}`.

**Dredd runs only in Docker.** No Node on host.

```bash
make docker-up    # start API on network linkup-net
make test-smoke   # Dredd container hits http://linkup-api:8181
make docker-down
```

Or run everything: **`make pre-commit`**.
