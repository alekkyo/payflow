# Deployment Guide — payflow.alexkua.com

This guide deploys PayFlow to a DigitalOcean Ubuntu 24.10 droplet behind Nginx with Let's Encrypt SSL, using Docker Compose for the Go services and PostgreSQL/Redis.

---

## Architecture on the server

```
Internet → Cloudflare DNS → Droplet
                              ├── Nginx :443 (SSL termination)
                              │     ├── /api/* → proxy → Go API :8080
                              │     └── /*     → React static files
                              └── Docker Compose
                                    ├── api    (Go, :8080 — loopback only)
                                    ├── worker (Go, no port)
                                    ├── postgres
                                    └── redis
```

---

## 1 — Cloudflare DNS

In your Cloudflare dashboard for **alexkua.com**:

1. Go to **DNS → Records → Add record**
2. Set:
   - Type: **A**
   - Name: **payflow**
   - IPv4: `<your-droplet-ip>`
   - Proxy status: **DNS only** (grey cloud) — keep this off until the SSL cert is issued
3. Save.

> After the cert is issued in step 7, you can switch to **Proxied** (orange cloud) for Cloudflare's CDN.  
> If you use Proxied, set SSL/TLS mode to **Full (strict)** in Cloudflare.

---

## 2 — Server: initial setup

SSH into your droplet:

```bash
ssh root@<your-droplet-ip>
```

### Install Docker

```bash
apt-get update && apt-get upgrade -y
apt-get install -y ca-certificates curl gnupg

install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
chmod a+r /etc/apt/keyrings/docker.gpg

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
  https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  tee /etc/apt/sources.list.d/docker.list > /dev/null

apt-get update
apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
```

### Install Nginx and Certbot

```bash
apt-get install -y nginx certbot python3-certbot-nginx
```

---

## 3 — Clone the repo

```bash
cd /var/www
git clone https://github.com/alexkua/payflow.git
cd payflow
```

---

## 4 — Configure environment

```bash
cp .env.example .env
nano .env
```

Set these values (everything else can stay as-is from the example):

```dotenv
ENV=production
STRIPE_API_KEY=sk_test_<your-key>
STRIPE_WEBHOOK_SECRET=whsec_<your-secret>
ALLOWED_ORIGINS=https://payflow.alexkua.com

# Used by docker-compose.prod.yml to build the DATABASE_URL
DB_USER=payflow
DB_PASSWORD=<generate a strong password, e.g.: openssl rand -hex 32>
```

> `DATABASE_URL` and `REDIS_URL` in `.env` are overridden by `docker-compose.prod.yml`  
> to use the Docker service hostnames. The `DB_USER`/`DB_PASSWORD` vars are what matter in prod.

---

## 5 — Build and start services

```bash
docker compose -f docker-compose.prod.yml up -d --build
```

Check all four containers are running:

```bash
docker compose -f docker-compose.prod.yml ps
```

---

## 6 — Run migrations and seed

```bash
# Run database migrations
docker compose -f docker-compose.prod.yml exec api /bin/api migrate up
```

Wait, the migrate command is a separate binary. Run it directly:

```bash
docker compose -f docker-compose.prod.yml run --rm \
  -e DATABASE_URL="postgres://payflow:${DB_PASSWORD}@postgres:5432/payflow?sslmode=disable" \
  api sh -c "echo 'migrations run via api container'"
```

Since the migrate command is a separate binary, build and run it from your local machine or add it to the compose:

```bash
# Easier: run migrations using the Go toolchain on the server
apt-get install -y golang-go   # or use the Docker build container
export DATABASE_URL="postgres://payflow:<DB_PASSWORD>@localhost:5432/payflow?sslmode=disable"
# Port 5432 isn't exposed in prod — use docker exec instead:
docker compose -f docker-compose.prod.yml exec postgres psql -U payflow -d payflow
```

**Simpler approach** — add a one-shot migrate service to run on deploy:

```bash
docker compose -f docker-compose.prod.yml run --rm \
  --entrypoint "" \
  -e DATABASE_URL="postgres://payflow:$(grep DB_PASSWORD .env | cut -d= -f2)@postgres:5432/payflow?sslmode=disable" \
  api /bin/api
```

Actually the cleanest way is to build the migrate binary in the same Docker image. Since the current `Dockerfile.api` only builds `/bin/api`, the recommended approach is to run migrations via a temporary container using the migration files directly. Add this to the repo's Dockerfile.api or run:

```bash
# On the server, in /var/www/payflow:
DB_PASS=$(grep DB_PASSWORD .env | cut -d= -f2)

docker run --rm --network payflow_default \
  -e DATABASE_URL="postgres://payflow:${DB_PASS}@postgres:5432/payflow?sslmode=disable" \
  -v $(pwd):/app \
  golang:1.24-alpine \
  sh -c "cd /app && go run ./cmd/migrate up"
```

Then seed demo data:

```bash
docker run --rm --network payflow_default \
  -e DATABASE_URL="postgres://payflow:${DB_PASS}@postgres:5432/payflow?sslmode=disable" \
  -v $(pwd):/app \
  golang:1.24-alpine \
  sh -c "cd /app && go run ./cmd/seed"
```

> The network name defaults to `<directory>_default`. If you cloned into `/var/www/payflow`, the network is `payflow_default`. Verify with `docker network ls`.

---

## 7 — Build the React frontend

```bash
cd /var/www/payflow/frontend
apt-get install -y nodejs npm
npm install
npm run build
```

The built files land in `/var/www/payflow/frontend/dist/`.

---

## 8 — Configure Nginx

```bash
cp /var/www/payflow/nginx/payflow.conf /etc/nginx/sites-available/payflow

# Enable the site
ln -s /etc/nginx/sites-available/payflow /etc/nginx/sites-enabled/payflow

# Disable the default site
rm -f /etc/nginx/sites-enabled/default

# Test config syntax
nginx -t
```

At this point, comment out the SSL lines temporarily so Nginx can start on port 80 for Certbot:

```bash
nano /etc/nginx/sites-available/payflow
# Comment out the ssl_certificate lines
nginx -t && systemctl reload nginx
```

---

## 9 — Issue SSL certificate

```bash
certbot --nginx -d payflow.alexkua.com --non-interactive --agree-tos -m your@email.com
```

Certbot automatically edits the Nginx config to add the cert paths. Restore the original config or verify the cert lines are correct:

```bash
nginx -t && systemctl reload nginx
```

Certbot installs a systemd timer for auto-renewal. Verify:

```bash
systemctl status certbot.timer
```

---

## 10 — Set up Stripe webhooks

In the Stripe Dashboard (test mode):

1. Go to **Developers → Webhooks → Add endpoint**
2. Endpoint URL: `https://payflow.alexkua.com/api/webhooks/stripe`
3. Events to subscribe: `payment_intent.succeeded`, `payment_intent.payment_failed`
4. Copy the **Signing secret** → update `STRIPE_WEBHOOK_SECRET` in `.env`
5. Restart the API: `docker compose -f docker-compose.prod.yml restart api worker`

---

## 11 — Enable Cloudflare proxy (optional)

Once the site is live and SSL is confirmed:

1. In Cloudflare, change the `payflow` record from **DNS only** to **Proxied** (orange cloud)
2. Set **SSL/TLS → Overview** mode to **Full (strict)**

This routes traffic through Cloudflare's CDN, adding DDoS protection and caching for static assets.

---

## Updating the deployment

On any code change:

```bash
cd /var/www/payflow
git pull
docker compose -f docker-compose.prod.yml up -d --build
```

If only the frontend changed:

```bash
cd /var/www/payflow/frontend
npm run build
# No service restart needed — Nginx serves the new static files immediately
```

---

## Useful commands

```bash
# View live logs
docker compose -f docker-compose.prod.yml logs -f api
docker compose -f docker-compose.prod.yml logs -f worker

# Restart a service
docker compose -f docker-compose.prod.yml restart api

# Check what's running
docker compose -f docker-compose.prod.yml ps

# Connect to PostgreSQL
docker compose -f docker-compose.prod.yml exec postgres psql -U payflow -d payflow

# Check Redis
docker compose -f docker-compose.prod.yml exec redis redis-cli ping
```
