import { useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { orders, payments, ApiError } from '../api/client'
import { OrderStatusTracker } from '../components/OrderStatusTracker'

// These statuses allow cancellation before payment processing begins.
const CANCELLABLE_STATUSES = new Set(['created', 'inventory_reserved'])

function formatPrice(cents: number, currency: string) {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency }).format(cents / 100)
}

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function OrderDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [cancelError, setCancelError] = useState('')

  const {
    data: order,
    isLoading,
    error,
  } = useQuery({
    queryKey: ['orders', id],
    queryFn: () => orders.getById(id!),
    enabled: !!id,
  })

  // Payment may not exist yet if the order is very new — retry: false avoids
  // flooding the backend with retries while the saga is still in progress.
  const { data: payment } = useQuery({
    queryKey: ['payment', id],
    queryFn: () => payments.getByOrderId(id!),
    enabled: !!id,
    retry: false,
  })

  const cancelMutation = useMutation({
    mutationFn: () => orders.cancel(id!),
    onSuccess: () => {
      // Invalidate both the list and this specific order so both pages reflect the new status.
      queryClient.invalidateQueries({ queryKey: ['orders'] })
      queryClient.invalidateQueries({ queryKey: ['orders', id] })
    },
    onError: (err) => {
      setCancelError(err instanceof ApiError ? err.message : 'Failed to cancel order')
    },
  })

  if (isLoading) {
    return (
      <div className="max-w-3xl mx-auto px-6 py-12 text-center text-gray-500">
        Loading order...
      </div>
    )
  }

  if (error || !order) {
    return (
      <div className="max-w-3xl mx-auto px-6 py-12 text-center text-red-600">
        Order not found.
      </div>
    )
  }

  return (
    <div className="max-w-3xl mx-auto px-6 py-8 space-y-6">
      <div className="flex items-center gap-4">
        <button
          onClick={() => navigate('/orders')}
          className="text-gray-400 hover:text-gray-600 text-sm transition-colors"
        >
          ← Back to orders
        </button>
        <h1 className="text-xl font-bold text-gray-900">
          Order #{order.id.slice(0, 8).toUpperCase()}
        </h1>
      </div>

      {/* Live status tracker — subscribes to SSE and updates as the saga progresses */}
      <OrderStatusTracker orderId={order.id} initialStatus={order.status} />

      {/* Order items */}
      <div className="bg-white border rounded-xl p-6">
        <h2 className="font-semibold text-gray-800 mb-4">Items</h2>
        <div className="divide-y">
          {order.items?.map((item, idx) => (
            <div key={idx} className="py-3 flex items-center justify-between">
              <div>
                <p className="font-medium text-gray-900">
                  {item.product_name ?? `Product ${item.product_id.slice(0, 8)}`}
                </p>
                <p className="text-sm text-gray-500">Qty: {item.quantity}</p>
              </div>
              <span className="font-semibold text-gray-900">
                {formatPrice(item.price_cents * item.quantity, order.currency)}
              </span>
            </div>
          ))}
        </div>
        <div className="pt-4 mt-2 border-t flex justify-between font-bold text-gray-900">
          <span>Total</span>
          <span>{formatPrice(order.total_cents, order.currency)}</span>
        </div>
        <p className="text-xs text-gray-400 mt-2">Placed {formatDateTime(order.created_at)}</p>
      </div>

      {/* Payment details — only shown once a payment record exists */}
      {payment && (
        <div className="bg-white border rounded-xl p-6">
          <h2 className="font-semibold text-gray-800 mb-4">Payment</h2>
          <dl className="space-y-2 text-sm">
            <div className="flex justify-between">
              <dt className="text-gray-500">Amount</dt>
              <dd className="font-medium">{formatPrice(payment.amount_cents, payment.currency)}</dd>
            </div>
            <div className="flex justify-between">
              <dt className="text-gray-500">Status</dt>
              <dd className="font-medium capitalize">{payment.status}</dd>
            </div>
            <div className="flex justify-between">
              <dt className="text-gray-500">Stripe ID</dt>
              <dd className="font-mono text-xs text-gray-600">{payment.stripe_payment_id}</dd>
            </div>
            {payment.failure_reason && (
              <div className="flex justify-between">
                <dt className="text-gray-500">Failure reason</dt>
                <dd className="text-red-600">{payment.failure_reason}</dd>
              </div>
            )}
          </dl>
        </div>
      )}

      {/* Cancel — only available before payment starts processing */}
      {CANCELLABLE_STATUSES.has(order.status) && (
        <div>
          {cancelError && (
            <p className="text-sm text-red-600 bg-red-50 px-3 py-2 rounded-lg mb-3">
              {cancelError}
            </p>
          )}
          <button
            onClick={() => cancelMutation.mutate()}
            disabled={cancelMutation.isPending}
            className="w-full border border-red-300 text-red-600 hover:bg-red-50 py-2 rounded-lg text-sm font-medium disabled:opacity-50 transition-colors"
          >
            {cancelMutation.isPending ? 'Cancelling...' : 'Cancel order'}
          </button>
        </div>
      )}
    </div>
  )
}
