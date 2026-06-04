package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/alexkua/payflow/internal/domain/product"
	redisstore "github.com/alexkua/payflow/internal/store/redis"
)

// ProductHandler handles HTTP requests for the product catalog.
type ProductHandler struct {
	store    product.Store
	invStore product.InventoryStore
	cache    *redisstore.ProductCache
	logger   *slog.Logger
}

// NewProductHandler creates a ProductHandler.
func NewProductHandler(store product.Store, invStore product.InventoryStore, cache *redisstore.ProductCache, logger *slog.Logger) *ProductHandler {
	return &ProductHandler{
		store:    store,
		invStore: invStore,
		cache:    cache,
		logger:   logger,
	}
}

// List handles GET /products — returns a paginated list of active products.
func (h *ProductHandler) List(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)

	// Cache-aside: check Redis first.
	if products, total, err := h.cache.GetProductList(r.Context(), page); err == nil {
		writeJSON(w, http.StatusOK, listResponse(products, total, page))
		return
	}

	products, total, err := h.store.List(r.Context(), page, 20)
	if err != nil {
		h.logger.Error("product.List store", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Populate cache; ignore error — a cache failure is not fatal for reads.
	_ = h.cache.SetProductList(r.Context(), page, products, total)

	writeJSON(w, http.StatusOK, listResponse(products, total, page))
}

// GetByID handles GET /products/:id.
func (h *ProductHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}

	// Cache-aside.
	if p, err := h.cache.GetProduct(r.Context(), id); err == nil {
		writeJSON(w, http.StatusOK, p)
		return
	}

	p, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, product.ErrNotFound) {
			writeError(w, http.StatusNotFound, "product not found")
			return
		}
		h.logger.Error("product.GetByID store", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	_ = h.cache.SetProduct(r.Context(), p)

	writeJSON(w, http.StatusOK, p)
}

// Create handles POST /products (admin only).
func (h *ProductHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req product.CreateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusUnprocessableEntity, "name is required")
		return
	}
	if req.PriceCents <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "price_cents must be positive")
		return
	}

	p, err := h.store.Create(r.Context(), req)
	if err != nil {
		h.logger.Error("product.Create store", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Invalidate list cache so the new product appears immediately.
	_ = h.cache.InvalidateProductList(r.Context())

	h.logger.Info("product created", "product_id", p.ID, "name", p.Name)
	writeJSON(w, http.StatusCreated, p)
}

// Update handles PUT /products/:id (admin only).
func (h *ProductHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}

	var req product.UpdateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	p, err := h.store.Update(r.Context(), id, req)
	if err != nil {
		if errors.Is(err, product.ErrNotFound) {
			writeError(w, http.StatusNotFound, "product not found")
			return
		}
		h.logger.Error("product.Update store", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Invalidate both the individual product and all list pages.
	_ = h.cache.DeleteProduct(r.Context(), id)
	_ = h.cache.InvalidateProductList(r.Context())

	h.logger.Info("product updated", "product_id", id)
	writeJSON(w, http.StatusOK, p)
}

// Deactivate handles DELETE /products/:id (admin only).
func (h *ProductHandler) Deactivate(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}

	if err := h.store.Deactivate(r.Context(), id); err != nil {
		if errors.Is(err, product.ErrNotFound) {
			writeError(w, http.StatusNotFound, "product not found")
			return
		}
		h.logger.Error("product.Deactivate store", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	_ = h.cache.DeleteProduct(r.Context(), id)
	_ = h.cache.InvalidateProductList(r.Context())

	h.logger.Info("product deactivated", "product_id", id)
	w.WriteHeader(http.StatusNoContent)
}

// SetInventory handles PUT /products/:id/inventory (admin only).
func (h *ProductHandler) SetInventory(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}

	var req struct {
		Quantity int `json:"quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Quantity < 0 {
		writeError(w, http.StatusUnprocessableEntity, "quantity must be non-negative")
		return
	}

	if err := h.invStore.SetQuantity(r.Context(), id, req.Quantity); err != nil {
		if errors.Is(err, product.ErrNotFound) {
			writeError(w, http.StatusNotFound, "product not found")
			return
		}
		h.logger.Error("product.SetInventory store", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.logger.Info("inventory updated", "product_id", id, "quantity", req.Quantity)
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

type listResponseBody struct {
	Products []*product.Product `json:"products"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
}

func listResponse(products []*product.Product, total, page int) listResponseBody {
	if products == nil {
		products = []*product.Product{}
	}
	return listResponseBody{Products: products, Total: total, Page: page}
}

func pageParam(r *http.Request) int {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if p < 1 {
		p = 1
	}
	return p
}

func uuidParam(w http.ResponseWriter, r *http.Request, key string) (uuid.UUID, bool) {
	raw := chi.URLParam(r, key)
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+key)
		return uuid.Nil, false
	}
	return id, true
}
