import { useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { useMutation } from '@tanstack/react-query'
import { orders, ApiError } from '../api/client'
import { useCartContext } from '../context/CartContext'

const SHIPPING_THRESHOLD = 20000
const SHIPPING_CENTS = 900

function fmt(cents: number) {
  return `$${Math.round(cents / 100)}`
}

export function Cart() {
  const { items, removeItem, updateQuantity, clear, totalCents } = useCartContext()
  const navigate = useNavigate()
  const [error, setError] = useState('')

  const shipping = totalCents >= SHIPPING_THRESHOLD ? 0 : SHIPPING_CENTS
  const total = totalCents + shipping

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

  if (items.length === 0) {
    return (
      <div className="max-w-[1180px] mx-auto px-6 pb-20 animate-pf-fade">
        <div className="pt-10 pb-6">
          <h1 className="font-heading text-[38px] m-0">Your cart</h1>
        </div>
        <div
          className="text-center py-20"
          style={{ background: '#ebddc5', borderRadius: 28 }}
        >
          <div
            className="w-16 h-16 rounded-full flex items-center justify-center mx-auto mb-5"
            style={{ background: '#fff2eb', color: '#8c491a' }}
          >
            <svg viewBox="0 0 24 24" width="28" height="28" fill="none" stroke="currentColor" strokeWidth="2.75" strokeLinecap="round" strokeLinejoin="round">
              <path d="M6 2 3 6v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V6l-3-4z" />
              <path d="M3 6h18" />
              <path d="M16 10a4 4 0 0 1-8 0" />
            </svg>
          </div>
          <div className="font-heading text-[24px] mb-1.5">Your cart is empty</div>
          <p className="m-0 mb-5" style={{ color: 'rgba(32,30,29,.55)' }}>Add a few things and watch the saga run.</p>
          <Link
            to="/"
            className="inline-flex items-center px-5 py-3 rounded-full font-heading text-[14px] no-underline transition-colors"
            style={{ background: '#c67139', color: '#f5ead8' }}
          >
            Browse products
          </Link>
        </div>
      </div>
    )
  }

  return (
    <div className="max-w-[1180px] mx-auto px-6 pb-20 animate-pf-fade">
      <div className="pt-10 pb-6">
        <h1 className="font-heading text-[38px] m-0">Your cart</h1>
      </div>

      <div
        className="gap-6 items-start"
        style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1.7fr) minmax(280px,1fr)' }}
      >
        {/* Item list */}
        <div className="flex flex-col gap-3.5">
          {items.map((item) => (
            <div
              key={item.product.id}
              className="flex gap-4 items-center px-4 py-3.5"
              style={{
                background: '#ebddc5',
                borderRadius: 28,
                boxShadow: '0 1px 2px rgba(46,43,37,.14)',
              }}
            >
              {/* Thumbnail */}
              <div
                className="flex-none rounded-[16px]"
                style={{
                  width: 74,
                  height: 74,
                  background: '#fff2eb',
                  backgroundImage:
                    'repeating-linear-gradient(-45deg, rgba(198,113,57,.08) 0 1px, transparent 1px 11px)',
                }}
              />

              {/* Name + meta */}
              <div className="flex-1 min-w-0">
                <div className="font-heading text-[16px] leading-[1.2]">{item.product.name}</div>
                <div className="text-[12.5px]" style={{ color: 'rgba(32,30,29,.55)' }}>
                  {fmt(item.product.price_cents)} each
                </div>
              </div>

              {/* Qty stepper */}
              <div
                className="inline-flex items-center gap-0.5 p-[3px] rounded-full"
                style={{ border: '1px solid rgba(32,30,29,.16)' }}
              >
                <button
                  aria-label="Decrease"
                  onClick={() => updateQuantity(item.product.id, item.quantity - 1)}
                  className="w-[30px] h-[30px] rounded-full flex items-center justify-center transition-colors"
                  style={{ background: 'transparent', border: 'none', cursor: 'pointer', color: '#201e1d' }}
                >
                  <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="2.75" strokeLinecap="round">
                    <path d="M5 12h14" />
                  </svg>
                </button>
                <span className="min-w-[22px] text-center font-heading text-[15px]">
                  {item.quantity}
                </span>
                <button
                  aria-label="Increase"
                  onClick={() => updateQuantity(item.product.id, item.quantity + 1)}
                  className="w-[30px] h-[30px] rounded-full flex items-center justify-center transition-colors"
                  style={{ background: 'transparent', border: 'none', cursor: 'pointer', color: '#201e1d' }}
                >
                  <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="2.75" strokeLinecap="round">
                    <path d="M5 12h14" />
                    <path d="M12 5v14" />
                  </svg>
                </button>
              </div>

              {/* Line total */}
              <div className="font-heading text-[18px] min-w-[64px] text-right">
                {fmt(item.product.price_cents * item.quantity)}
              </div>

              {/* Remove */}
              <button
                aria-label="Remove"
                onClick={() => removeItem(item.product.id)}
                className="p-1.5 rounded-full transition-colors"
                style={{ background: 'transparent', border: 'none', cursor: 'pointer', color: '#c67139' }}
              >
                <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" strokeWidth="2.75" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M3 6h18" />
                  <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
                  <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
                </svg>
              </button>
            </div>
          ))}
        </div>

        {/* Summary card */}
        <div
          className="flex flex-col gap-3.5"
          style={{
            position: 'sticky',
            top: 88,
            background: '#ebddc5',
            borderRadius: 28,
            padding: 22,
            boxShadow: '0 3px 10px rgba(46,43,37,.16)',
          }}
        >
          <h3 className="font-heading text-[20px] m-0">Summary</h3>

          <div className="flex justify-between text-[14px]">
            <span style={{ color: 'rgba(32,30,29,.55)' }}>Subtotal</span>
            <span>{fmt(totalCents)}</span>
          </div>

          <div className="flex justify-between text-[14px]">
            <span style={{ color: 'rgba(32,30,29,.55)' }}>Shipping</span>
            <span>{shipping === 0 ? 'Free' : fmt(shipping)}</span>
          </div>

          {shipping > 0 && (
            <div className="text-[12px] -mt-1.5" style={{ color: 'rgba(32,30,29,.55)' }}>
              Add {fmt(SHIPPING_THRESHOLD - totalCents)} more for free shipping
            </div>
          )}

          <hr className="my-1.5 border-0 h-px" style={{ background: 'rgba(32,30,29,.16)' }} />

          <div className="flex justify-between items-baseline">
            <span className="font-heading text-[17px]">Total</span>
            <span className="font-heading text-[26px]">{fmt(total)}</span>
          </div>

          {error && (
            <p className="text-[13px] m-0 px-3 py-2 rounded-full" style={{ background: '#fff2eb', color: '#8c491a' }}>
              {error}
            </p>
          )}

          <button
            onClick={() => { setError(''); checkout.mutate() }}
            disabled={checkout.isPending}
            className="w-full py-3 rounded-full font-heading text-[15px] transition-colors mt-1.5"
            style={{
              background: '#c67139',
              color: '#f5ead8',
              border: 'none',
              cursor: checkout.isPending ? 'not-allowed' : 'pointer',
              opacity: checkout.isPending ? 0.7 : 1,
            }}
          >
            {checkout.isPending ? 'Placing order…' : 'Place order'}
          </button>

          <div
            className="flex items-center justify-center gap-1.5 text-[11px]"
            style={{ color: 'rgba(32,30,29,.55)' }}
          >
            <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2.75" strokeLinecap="round" strokeLinejoin="round">
              <rect x="3" y="11" width="18" height="11" rx="2" />
              <path d="M7 11V7a5 5 0 0 1 10 0v4" />
            </svg>
            Secured by Stripe · idempotency-key sent
          </div>
        </div>
      </div>
    </div>
  )
}
