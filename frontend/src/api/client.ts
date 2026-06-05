// Base URL: in dev the Vite proxy rewrites /api/* → http://localhost:8080/*
// In production set VITE_API_URL to your deployed API origin.
const BASE_URL = import.meta.env.VITE_API_URL ?? '/api'

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const token = localStorage.getItem('token')

  const res = await fetch(`${BASE_URL}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...init.headers,
    },
  })

  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }))
    throw new ApiError(res.status, body.error ?? 'Request failed')
  }

  // 204 No Content has no body
  if (res.status === 204) return undefined as T
  return res.json()
}

// ── Auth ─────────────────────────────────────────────────────────────────────

export type User = { id: string; email: string; name: string; role: string }
export type AuthResponse = { token: string; user: User }

export const auth = {
  register: (email: string, password: string, name: string) =>
    request<AuthResponse>('/auth/register', {
      method: 'POST',
      body: JSON.stringify({ email, password, name }),
    }),

  login: (email: string, password: string) =>
    request<AuthResponse>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    }),

  logout: () => request<void>('/auth/logout', { method: 'POST' }),
}

// ── Products ─────────────────────────────────────────────────────────────────

export type Product = {
  id: string
  name: string
  description: string
  price_cents: number
  currency: string
  active: boolean
}

export type InventoryLevel = {
  product_id: string
  quantity: number
  reserved: number
  available: number
}

export const products = {
  list: () => request<{ products: Product[] }>('/products'),
  getById: (id: string) => request<Product>(`/products/${id}`),
  // Returns available stock per product_id. No auth required — safe to poll.
  inventory: () => request<{ inventory: Record<string, number> }>('/products/inventory'),
}

// ── Orders ───────────────────────────────────────────────────────────────────

export type OrderItem = {
  product_id: string
  quantity: number
  price_cents: number
  product_name?: string
}

export type Order = {
  id: string
  user_id: string
  status: string
  total_cents: number
  currency: string
  items: OrderItem[]
  created_at: string
  updated_at: string
}

export const orders = {
  create: (items: { product_id: string; quantity: number }[], idempotencyKey: string) =>
    request<Order>('/orders', {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify({ items }),
    }),

  list: () => request<{ orders: Order[] }>('/orders'),

  getById: (id: string) => request<Order>(`/orders/${id}`),

  cancel: (id: string) => request<void>(`/orders/${id}/cancel`, { method: 'POST' }),
}

// ── Payments ─────────────────────────────────────────────────────────────────

export type Payment = {
  id: string
  order_id: string
  stripe_payment_id: string
  stripe_dashboard_url?: string
  amount_cents: number
  currency: string
  status: string
  failure_reason?: string
  created_at: string
}

export type Refund = {
  id: string
  payment_id: string
  amount_cents: number
  reason?: string
  status: string
  created_at: string
}

export const payments = {
  getByOrderId: (orderId: string) => request<Payment>(`/orders/${orderId}/payment`),

  createRefund: (orderId: string, amountCents: number, reason: string, idempotencyKey: string) =>
    request<Refund>(`/orders/${orderId}/refunds`, {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify({ amount_cents: amountCents, reason }),
    }),

  listRefunds: (orderId: string) => request<Refund[]>(`/orders/${orderId}/refunds`),
}

// ── Admin ────────────────────────────────────────────────────────────────────

export type ReconciliationRun = {
  id: string
  run_date: string
  status: string
  matched: number
  mismatched: number
  missing_local: number
  missing_stripe: number
  started_at: string
  completed_at?: string
}

export const admin = {
  listReconciliationRuns: () =>
    request<ReconciliationRun[]>('/admin/reconciliation/runs'),

  triggerReconciliation: (date?: string) => {
    const params = date ? `?date=${date}` : ''
    return request<{ message: string }>(`/admin/reconciliation/trigger${params}`, {
      method: 'POST',
    })
  },

  getQueueDepths: () => request<Record<string, number>>('/admin/queues'),

  listDeadLetterMessages: () =>
    request<{ count: number; messages: { id: string; fields: Record<string, string> }[] }>(
      '/admin/deadletter',
    ),
}
