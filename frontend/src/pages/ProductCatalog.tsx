import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { products } from '../api/client'
import { useCartContext } from '../context/CartContext'
import { useAuth } from '../hooks/useAuth'

function formatPrice(cents: number, currency: string) {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency }).format(cents / 100)
}

export function ProductCatalog() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['products'],
    queryFn: () => products.list(),
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
            return (
              <div
                key={product.id}
                className="bg-white border rounded-xl p-6 flex flex-col gap-3 shadow-sm hover:shadow-md transition-shadow"
              >
                <h2 className="font-semibold text-gray-900">{product.name}</h2>
                <p className="text-sm text-gray-500 flex-1">{product.description}</p>
                <div className="flex items-center justify-between mt-2">
                  <span className="text-lg font-bold text-gray-900">
                    {formatPrice(product.price_cents, product.currency)}
                  </span>
                  {user ? (
                    <button
                      onClick={() => addItem(product)}
                      className="bg-indigo-600 hover:bg-indigo-500 text-white px-4 py-1.5 rounded-lg text-sm font-medium transition-colors"
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
