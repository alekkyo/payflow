import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { orders } from '../api/client'
import { ORDER_STATUS_LABELS } from '../hooks/useOrderStream'

function formatPrice(cents: number, currency: string) {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency }).format(cents / 100)
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

const STATUS_COLORS: Record<string, string> = {
  created: 'bg-gray-100 text-gray-700',
  inventory_reserved: 'bg-blue-100 text-blue-700',
  payment_processing: 'bg-blue-100 text-blue-700',
  payment_captured: 'bg-green-100 text-green-700',
  confirmed: 'bg-green-100 text-green-700',
  fulfilled: 'bg-green-100 text-green-700',
  payment_failed: 'bg-red-100 text-red-700',
  cancelled: 'bg-red-100 text-red-700',
  refunded: 'bg-yellow-100 text-yellow-700',
}

export function OrderList() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['orders'],
    queryFn: () => orders.list(),
  })

  if (isLoading) {
    return (
      <div className="max-w-4xl mx-auto px-6 py-12 text-center text-gray-500">
        Loading orders...
      </div>
    )
  }

  if (error) {
    return (
      <div className="max-w-4xl mx-auto px-6 py-12 text-center text-red-600">
        Failed to load orders.
      </div>
    )
  }

  const orderList = data?.orders ?? []

  return (
    <div className="max-w-4xl mx-auto px-6 py-8">
      <h1 className="text-2xl font-bold text-gray-900 mb-6">My Orders</h1>

      {orderList.length === 0 ? (
        <div className="text-center py-16">
          <p className="text-gray-500 mb-4">You haven't placed any orders yet.</p>
          <Link to="/" className="text-indigo-600 hover:underline">
            Browse products
          </Link>
        </div>
      ) : (
        <div className="space-y-3">
          {orderList.map((order) => (
            <Link
              key={order.id}
              to={`/orders/${order.id}`}
              className="block bg-white border rounded-xl px-6 py-4 hover:border-indigo-300 transition-colors"
            >
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium text-gray-900">
                    Order #{order.id.slice(0, 8).toUpperCase()}
                  </p>
                  <p className="text-sm text-gray-500 mt-0.5">
                    {formatDate(order.created_at)} · {order.items?.length ?? 0} item
                    {(order.items?.length ?? 0) !== 1 ? 's' : ''}
                  </p>
                </div>
                <div className="flex items-center gap-4">
                  <span className="font-semibold text-gray-900">
                    {formatPrice(order.total_cents, order.currency)}
                  </span>
                  <span
                    className={`text-xs font-medium px-2.5 py-1 rounded-full ${
                      STATUS_COLORS[order.status] ?? 'bg-gray-100 text-gray-700'
                    }`}
                  >
                    {ORDER_STATUS_LABELS[order.status] ?? order.status}
                  </span>
                </div>
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}
