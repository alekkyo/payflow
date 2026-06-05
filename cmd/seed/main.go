// cmd/seed populates the database with demo users, products, and inventory.
// Run with: go run ./cmd/seed  (requires DATABASE_URL to be set)
// Safe to run multiple times — uses ON CONFLICT DO NOTHING for users and
// skips product insertion if the catalog is already populated.
package main

import (
	"context"
	"log/slog"
	"os"

	"golang.org/x/crypto/bcrypt"

	"github.com/alexkua/payflow/internal/config"
	"github.com/alexkua/payflow/internal/domain/product"
	pgstore "github.com/alexkua/payflow/internal/store/postgres"
)

// Demo credentials — also shown on the login page.
const (
	adminEmail    = "admin@payflow.dev"
	adminPassword = "demo-admin-123"

	customerEmail    = "customer@payflow.dev"
	customerPassword = "demo-customer-123"
)

type seedItem struct {
	product.CreateProductRequest
	inventory int
}

var catalog = []seedItem{
	{
		CreateProductRequest: product.CreateProductRequest{
			Name:        "Wireless Headphones",
			Description: "Premium noise-cancelling headphones with 30-hour battery life and foldable design.",
			PriceCents:  19999,
			Currency:    "usd",
		},
		inventory: 25,
	},
	{
		CreateProductRequest: product.CreateProductRequest{
			Name:        "Mechanical Keyboard",
			Description: "Compact TKL layout with Cherry MX Blue switches and per-key RGB backlighting.",
			PriceCents:  12999,
			Currency:    "usd",
		},
		inventory: 7,
	},
	{
		CreateProductRequest: product.CreateProductRequest{
			Name:        "USB-C Hub 7-in-1",
			Description: "HDMI 4K, 3× USB-A, SD card reader, and 100W pass-through charging in one.",
			PriceCents:  4999,
			Currency:    "usd",
		},
		inventory: 5,
	},
	{
		CreateProductRequest: product.CreateProductRequest{
			Name:        "4K Webcam",
			Description: "30fps 4K autofocus webcam with built-in ring light and noise-cancelling mic.",
			PriceCents:  8999,
			Currency:    "usd",
		},
		inventory: 50,
	},
	{
		CreateProductRequest: product.CreateProductRequest{
			Name:        "Desk Pad XL",
			Description: "90 × 40 cm extended mouse pad with anti-slip base and stitched edges.",
			PriceCents:  2999,
			Currency:    "usd",
		},
		inventory: 8,
	},
	{
		CreateProductRequest: product.CreateProductRequest{
			Name:        "Cable Management Kit",
			Description: "20-piece bundle of magnetic cable clips and reusable velcro ties.",
			PriceCents:  1499,
			Currency:    "usd",
		},
		inventory: 3,
	},
	{
		CreateProductRequest: product.CreateProductRequest{
			Name:        "Ergonomic Monitor Arm",
			Description: "Full-motion single monitor arm supporting screens up to 32\" and 8 kg.",
			PriceCents:  7999,
			Currency:    "usd",
		},
		inventory: 0,
	},
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

	pool, err := pgstore.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("connecting to postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// ── Users ──────────────────────────────────────────────────────────────────

	type demoUser struct {
		email, password, role string
	}
	users := []demoUser{
		{adminEmail, adminPassword, "admin"},
		{customerEmail, customerPassword, "customer"},
	}

	for _, u := range users {
		hash, err := bcrypt.GenerateFromPassword([]byte(u.password), 12)
		if err != nil {
			slog.Error("hash password", "email", u.email, "error", err)
			os.Exit(1)
		}

		// ON CONFLICT DO NOTHING makes the seeder idempotent — re-running it
		// won't duplicate or overwrite existing accounts.
		const q = `
			INSERT INTO users (email, password_hash, role)
			VALUES ($1, $2, $3)
			ON CONFLICT (email) DO NOTHING`

		if _, err := pool.Exec(ctx, q, u.email, string(hash), u.role); err != nil {
			slog.Error("insert user", "email", u.email, "error", err)
			os.Exit(1)
		}
		slog.Info("seeded user", "email", u.email, "role", u.role)
	}

	// ── Products ───────────────────────────────────────────────────────────────

	productStore := pgstore.NewProductStore(pool)
	inventoryStore := pgstore.NewInventoryStore(pool)

	// Skip if products already exist so re-running doesn't double the catalog.
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM products`).Scan(&count); err != nil {
		slog.Error("count products", "error", err)
		os.Exit(1)
	}
	if count > 0 {
		slog.Info("products already seeded, skipping", "existing", count)
	} else {
		for _, item := range catalog {
			p, err := productStore.Create(ctx, item.CreateProductRequest)
			if err != nil {
				slog.Error("create product", "name", item.Name, "error", err)
				os.Exit(1)
			}
			if err := inventoryStore.SetQuantity(ctx, p.ID, item.inventory); err != nil {
				slog.Error("set inventory", "name", item.Name, "error", err)
				os.Exit(1)
			}
			slog.Info("seeded product", "name", p.Name, "inventory", item.inventory)
		}
		slog.Info("products seeded", "count", len(catalog))
	}

	slog.Info("seeding complete")
}
