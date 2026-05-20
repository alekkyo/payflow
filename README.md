@"
# PayFlow

Distributed payment processing platform built in Go.

See [ARCHITECTURE.md](./ARCHITECTURE.md) for full system design.

## Stack
- Go 1.24 — API and worker services
- PostgreSQL 16 — primary data store
- Redis 7 — caching, distributed locks, queue
- Stripe — payment provider
- React 19 — frontend
- Docker + AWS ECS Fargate — deployment

## Status
🚧 In active development
"@ | Out-File -FilePath "README.md" -Encoding utf8