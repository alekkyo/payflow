import { useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { useMutation } from '@tanstack/react-query'
import { orders, ApiError } from '../api/client'
import { useCartContext } from '../context/CartContext'
import { useAuth } from '../hooks/useAuth'

function formatPrice(cents: number) {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(cents / 100)
}

export function Cart() {
  const { items, removeItem, clear, totalCents } = useCartContext()
  const { user } = useAuth()
  const navigate = useNavigate()
  const [error, setError] = useState('')

  // crypto.randomUUID() generates a v4 UUID for the idempotency key.
  // If the user double-clicks "Place order", the second request hits the
  // backend with the same key and returns the already-created order instead
  // of creating a duplicate.
  const checkout = useMutation({
    mutationFn: () => {
      const idempotencyKey = crypto.randomUUID()
      return orders.create(
        items.map((i) => ({ product_id: i.product.id, quantity: i.quantity })),
        idempotencyKey,
      )
    },
    onSuccess: (order) => {
      clear()
      navigate(`/orders/${order.id}`)
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : 'Checkout failed. Please try again.')
    },
  })

  if (!user) {
    return (
      <div className="max-w-2xl mx-auto px-6 py-16 text-center">
        <p className="text-gray-500 mb-4">Sign in to checkout.</p>
        <Link to="/login" className="text-indigo-600 hover:underline">
          Go to sign in
        </Link>
      </div>
    )
  }

  if (items.length === 0) {
    return (
      <div className="max-w-2xl mx-auto px-6 py-16 text-center">
        <p className="text-gray-500 mb-4">Your cart is empty.</p>
        <Link to="/" className="text-indigo-600 hover:underline">
          Browse products
        </Link>
      </div>
    )
  }

  return (
    <div className="max-w-2xl mx-auto px-6 py-8">
      <h1 className="text-2xl font-bold text-gray-900 mb-6">Your Cart</h1>

      <div className="bg-white border rounded-xl divide-y">
        {items.map((item) => (
          <div key={item.product.id} className="flex items-center justify-between px-6 py-4">
            <div>
              <p className="font-medium text-gray-900">{item.product.name}</p>
              <p className="text-sm text-gray-500">
                {formatPrice(item.product.price_cents)} × {item.quantity}
              </p>
            </div>
            <div className="flex items-center gap-4">
              <span className="font-semibold text-gray-900">
                {formatPrice(item.product.price_cents * item.quantity)}
              </span>
              <button
                onClick={() => removeItem(item.product.id)}
                className="text-gray-400 hover:text-red-500 text-sm transition-colors"
              >
                Remove
              </button>
            </div>
          </div>
        ))}
      </div>

      <div className="mt-6 flex items-center justify-between">
        <span className="text-lg font-semibold text-gray-900">Total</span>
        <span className="text-xl font-bold text-gray-900">{formatPrice(totalCents)}</span>
      </div>

      {error && (
        <p className="mt-4 text-sm text-red-600 bg-red-50 px-3 py-2 rounded-lg">{error}</p>
      )}

      <button
        onClick={() => { setError(''); checkout.mutate() }}
        disabled={checkout.isPending}
        className="mt-6 w-full bg-indigo-600 hover:bg-indigo-500 text-white py-3 rounded-xl font-semibold disabled:opacity-50 transition-colors"
      >
        {checkout.isPending ? 'Placing order...' : 'Place order'}
      </button>
    </div>
  )
}
