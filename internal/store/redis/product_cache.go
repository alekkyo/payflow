package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/alexkua/payflow/internal/domain/product"
)

const (
	productTTL     = 10 * time.Minute
	productListTTL = 5 * time.Minute
)

// ProductCache provides Redis-backed caching for the product catalog.
type ProductCache struct {
	client *redis.Client
}

// NewProductCache creates a ProductCache using the given Redis client.
func NewProductCache(client *redis.Client) *ProductCache {
	return &ProductCache{client: client}
}

// GetProduct retrieves a single product from cache. Returns ErrCacheMiss if not found.
func (c *ProductCache) GetProduct(ctx context.Context, id uuid.UUID) (*product.Product, error) {
	key := fmt.Sprintf("product:%s", id)
	data, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, fmt.Errorf("product_cache.GetProduct: %w", err)
	}

	var p product.Product
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("product_cache.GetProduct unmarshal: %w", err)
	}
	return &p, nil
}

// SetProduct stores a product in cache with a 10-minute TTL.
func (c *ProductCache) SetProduct(ctx context.Context, p *product.Product) error {
	key := fmt.Sprintf("product:%s", p.ID)
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("product_cache.SetProduct marshal: %w", err)
	}
	if err := c.client.Set(ctx, key, data, productTTL).Err(); err != nil {
		return fmt.Errorf("product_cache.SetProduct: %w", err)
	}
	return nil
}

// DeleteProduct removes a product from cache.
func (c *ProductCache) DeleteProduct(ctx context.Context, id uuid.UUID) error {
	key := fmt.Sprintf("product:%s", id)
	if err := c.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("product_cache.DeleteProduct: %w", err)
	}
	return nil
}

// productListResponse is the cached shape for a product list page.
type productListResponse struct {
	Products []*product.Product `json:"products"`
	Total    int                `json:"total"`
}

// GetProductList retrieves a paginated product list from cache.
func (c *ProductCache) GetProductList(ctx context.Context, page int) ([]*product.Product, int, error) {
	key := fmt.Sprintf("products:catalog:page:%d", page)
	data, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, 0, ErrCacheMiss
	}
	if err != nil {
		return nil, 0, fmt.Errorf("product_cache.GetProductList: %w", err)
	}

	var resp productListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, 0, fmt.Errorf("product_cache.GetProductList unmarshal: %w", err)
	}
	return resp.Products, resp.Total, nil
}

// SetProductList stores a product list page in cache with a 5-minute TTL.
func (c *ProductCache) SetProductList(ctx context.Context, page int, products []*product.Product, total int) error {
	key := fmt.Sprintf("products:catalog:page:%d", page)
	data, err := json.Marshal(productListResponse{Products: products, Total: total})
	if err != nil {
		return fmt.Errorf("product_cache.SetProductList marshal: %w", err)
	}
	if err := c.client.Set(ctx, key, data, productListTTL).Err(); err != nil {
		return fmt.Errorf("product_cache.SetProductList: %w", err)
	}
	return nil
}

// InvalidateProductList removes all cached product list pages using SCAN.
func (c *ProductCache) InvalidateProductList(ctx context.Context) error {
	pattern := "products:catalog:page:*"
	var cursor uint64
	for {
		keys, next, err := c.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("product_cache.InvalidateProductList scan: %w", err)
		}
		if len(keys) > 0 {
			if err := c.client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("product_cache.InvalidateProductList del: %w", err)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}

// ErrCacheMiss is returned when a key is not present in the cache.
var ErrCacheMiss = errors.New("cache miss")
