import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { products } from '../api/client'
import { useCartContext } from '../context/CartContext'
import { useAuth } from '../hooks/useAuth'

function formatPrice(cents: number, currency: string) {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency }).format(cents / 100)
}

const LOW_STOCK_THRESHOLD = 10

export function ProductCatalog() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['products'],
    queryFn: () => products.list(),
  })
  // Inventory levels poll every 30 s so the "Only X left!" badge stays current
  // as other customers buy. staleTime: 0 means the cache is immediately stale,
  // prompting a background refetch on every focus or interval.
  const { data: invData } = useQuery({
    queryKey: ['products', 'inventory'],
    queryFn: () => products.inventory(),
    refetchInterval: 30_000,
    staleTime: 0,
  })
  const { addItem, items, totalCents } = useCartContext()
  const { user } = useAuth()

  if (isLoading) {
    return (
      <div className="max-w-6xl mx-auto px-6 py-12 text-center text-gray-500">
        Loading products...
      </div>
    )
  }

  if (error) {
    return (
      <div className="max-w-6xl mx-auto px-6 py-12 text-center text-red-600">
        Failed to load products.
      </div>
    )
  }

  const productList = (data?.products ?? []).filter((p) => p.active)
  const cartCount = items.reduce((sum, i) => sum + i.quantity, 0)

  return (
    <div className="max-w-6xl mx-auto px-6 py-8">
      <div className="flex items-center justify-between mb-8">
        <h1 className="text-2xl font-bold text-gray-900">Products</h1>
        {cartCount > 0 && (
          <Link
            to="/cart"
            className="flex items-center gap-2 bg-indigo-600 hover:bg-indigo-500 text-white px-4 py-2 rounded-lg text-sm font-medium"
          >
            <span>Cart ({cartCount})</span>
            <span className="text-indigo-200">·</span>
            <span>{formatPrice(totalCents, 'USD')}</span>
          </Link>
        )}
      </div>

      {productList.length === 0 ? (
        <p className="text-gray-500">No products available.</p>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-6">
          {productList.map((product) => {
            const inCart = items.find((i) => i.product.id === product.id)
            const available = invData?.inventory?.[product.id]
            const isLowStock = available !== undefined && available < LOW_STOCK_THRESHOLD
            const isOutOfStock = available !== undefined && available <= 0
            return (
              <div
                key={product.id}
                className="bg-white border rounded-xl p-6 flex flex-col gap-3 shadow-sm hover:shadow-md transition-shadow"
              >
                <div className="flex items-start justify-between gap-2">
                  <h2 className="font-semibold text-gray-900">{product.name}</h2>
                  {isLowStock && !isOutOfStock && (
                    <span className="shrink-0 text-xs font-medium text-orange-600 bg-orange-50 border border-orange-200 px-2 py-0.5 rounded-full">
                      Only {available} left!
                    </span>
                  )}
                  {isOutOfStock && (
                    <span className="shrink-0 text-xs font-medium text-red-600 bg-red-50 border border-red-200 px-2 py-0.5 rounded-full">
                      Out of stock
                    </span>
                  )}
                </div>
                <p className="text-sm text-gray-500 flex-1">{product.description}</p>
                <div className="flex items-center justify-between mt-2">
                  <span className="text-lg font-bold text-gray-900">
                    {formatPrice(product.price_cents, product.currency)}
                  </span>
                  {user ? (
                    <button
                      onClick={() => addItem(product)}
                      disabled={isOutOfStock}
                      className="bg-indigo-600 hover:bg-indigo-500 text-white px-4 py-1.5 rounded-lg text-sm font-medium transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                    >
                      {inCart ? `In cart (${inCart.quantity})` : 'Add to cart'}
                    </button>
                  ) : (
                    <Link to="/login" className="text-indigo-600 hover:underline text-sm">
                      Sign in to buy
                    </Link>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
