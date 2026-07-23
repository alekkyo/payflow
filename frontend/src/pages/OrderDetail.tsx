import { useState, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { orders, payments, ApiError } from '../api/client'
import { OrderStatusTracker } from '../components/OrderStatusTracker'

const CANCELLABLE = new Set(['created', 'inventory_reserved'])

function fmt(cents: number, currency = 'USD') {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency }).format(cents / 100)
}

function fmtDt(iso: string) {
  return new Date(iso).toLocaleString('en-US', {
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  })
}

export function OrderDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [cancelError, setCancelError] = useState('')
  const [liveStatus, setLiveStatus] = useState<string | null>(null)

  const { data: order, isLoading, error } = useQuery({
    queryKey: ['orders', id],
    queryFn: () => orders.getById(id!),
    enabled: !!id,
  })

  const { data: payment } = useQuery({
    queryKey: ['payment', id],
    queryFn: () => payments.getByOrderId(id!),
    enabled: !!id,
    retry: false,
  })

  const handleStatusChange = useCallback((newStatus: string) => {
    setLiveStatus(newStatus)
    queryClient.invalidateQueries({ queryKey: ['orders', id] })
    queryClient.invalidateQueries({ queryKey: ['payment', id] })
    queryClient.invalidateQueries({ queryKey: ['orders'] })
  }, [id, queryClient])

  const cancelMutation = useMutation({
    mutationFn: () => orders.cancel(id!),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['orders'] })
      queryClient.invalidateQueries({ queryKey: ['orders', id] })
    },
    onError: (err) => {
      setCancelError(err instanceof ApiError ? err.message : 'Failed to cancel order')
    },
  })

  if (isLoading) {
    return (
      <div className="max-w-[1180px] mx-auto px-6 pb-20">
        <div className="pt-10 pb-6">
          <div className="pf-shim h-10 w-64" />
        </div>
        <div className="pf-shim h-[480px] rounded-[28px]" />
      </div>
    )
  }

  if (error || !order) {
    return (
      <div className="max-w-[1180px] mx-auto px-6 py-16 text-center" style={{ color: '#8c491a' }}>
        Order not found.
      </div>
    )
  }

  const currentStatus = liveStatus ?? order.status

  return (
    <div className="max-w-[1180px] mx-auto px-6 pb-20 animate-pf-fade">
      {/* Back nav + heading */}
      <div className="pt-10 pb-6 flex items-center gap-4">
        <button
          onClick={() => navigate('/orders')}
          className="text-[14px] transition-colors font-heading"
          style={{ background: 'transparent', border: 'none', cursor: 'pointer', color: 'rgba(32,30,29,.55)' }}
        >
          ← Orders
        </button>
        <h1 className="font-heading text-[32px] m-0">
          Order #{order.id.slice(0, 8).toUpperCase()}
        </h1>
        <span className="text-[11px] px-2.5 py-0.5 rounded-full font-heading" style={{ background: '#f0fae1', color: '#3d472b' }}>
          {fmtDt(order.created_at)}
        </span>
      </div>

      <div className="flex flex-col gap-5">
        {/* Hero: saga timeline */}
        <div style={{ background: '#ebddc5', borderRadius: 28, boxShadow: '0 3px 10px rgba(46,43,37,.16)', overflow: 'hidden' }}>
          <OrderStatusTracker
            orderId={order.id}
            initialStatus={order.status}
            onStatusChange={handleStatusChange}
          />
        </div>

        {/* Items + Payment side by side on wide */}
        <div
          className="gap-5 items-start"
          style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))' }}
        >
          {/* Order items */}
          <div style={{ background: '#ebddc5', borderRadius: 28, padding: '22px 24px', boxShadow: '0 1px 2px rgba(46,43,37,.14)' }}>
            <h4 className="font-heading text-[17px] m-0 mb-4">Items</h4>
            <div className="flex flex-col" style={{ gap: 0 }}>
              {order.items?.map((item, idx) => (
                <div
                  key={idx}
                  className="py-3 flex items-center justify-between"
                  style={{ borderBottom: '1px solid rgba(32,30,29,.08)' }}
                >
                  <div>
                    <div className="font-heading text-[15px]">{item.product_name}</div>
                    <div className="text-[12.5px]" style={{ color: 'rgba(32,30,29,.55)' }}>Qty: {item.quantity}</div>
                  </div>
                  <span className="font-heading text-[17px]">
                    {fmt(item.price_cents * item.quantity, order.currency)}
                  </span>
                </div>
              ))}
            </div>
            <div className="flex justify-between items-baseline pt-4 mt-2" style={{ borderTop: '1px solid rgba(32,30,29,.16)' }}>
              <span className="font-heading text-[17px]">Total</span>
              <span className="font-heading text-[22px]">{fmt(order.total_cents, order.currency)}</span>
            </div>
          </div>

          {/* Payment */}
          {payment && (
            <div style={{ background: '#ebddc5', borderRadius: 28, padding: '22px 24px', boxShadow: '0 1px 2px rgba(46,43,37,.14)' }}>
              <h4 className="font-heading text-[17px] m-0 mb-4">Payment</h4>
              <dl className="flex flex-col gap-2 text-[14px]">
                <div className="flex justify-between">
                  <dt style={{ color: 'rgba(32,30,29,.55)' }}>Amount</dt>
                  <dd className="m-0 font-medium">{fmt(payment.amount_cents, payment.currency)}</dd>
                </div>
                <div className="flex justify-between">
                  <dt style={{ color: 'rgba(32,30,29,.55)' }}>Status</dt>
                  <dd className="m-0 font-medium capitalize">{payment.status}</dd>
                </div>
                <div className="flex justify-between">
                  <dt style={{ color: 'rgba(32,30,29,.55)' }}>Stripe ID</dt>
                  <dd className="m-0 text-[12px]" style={{ fontFamily: 'ui-monospace, monospace', color: 'rgba(32,30,29,.6)' }}>
                    {payment.stripe_payment_id}
                  </dd>
                </div>
                {payment.failure_reason && (
                  <div className="flex justify-between">
                    <dt style={{ color: 'rgba(32,30,29,.55)' }}>Failure</dt>
                    <dd className="m-0" style={{ color: '#8c491a' }}>{payment.failure_reason}</dd>
                  </div>
                )}
              </dl>
              {payment.stripe_dashboard_url && (
                <a
                  href={payment.stripe_dashboard_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="mt-4 flex items-center gap-1.5 text-[13px] no-underline font-heading transition-colors"
                  style={{ color: '#c67139' }}
                >
                  View on Stripe
                  <svg viewBox="0 0 20 20" fill="currentColor" width="14" height="14">
                    <path fillRule="evenodd" d="M4.25 5.5a.75.75 0 00-.75.75v8.5c0 .414.336.75.75.75h8.5a.75.75 0 00.75-.75v-4a.75.75 0 011.5 0v4A2.25 2.25 0 0112.75 17h-8.5A2.25 2.25 0 012 14.75v-8.5A2.25 2.25 0 014.25 4h5a.75.75 0 010 1.5h-5z" clipRule="evenodd" />
                    <path fillRule="evenodd" d="M6.194 12.753a.75.75 0 001.06.053L16.5 4.44v2.81a.75.75 0 001.5 0v-4.5a.75.75 0 00-.75-.75h-4.5a.75.75 0 000 1.5h2.553l-9.056 8.194a.75.75 0 00-.053 1.06z" clipRule="evenodd" />
                  </svg>
                </a>
              )}
            </div>
          )}
        </div>

        {/* Cancel */}
        {CANCELLABLE.has(currentStatus) && (
          <div>
            {cancelError && (
              <p className="text-[13px] px-4 py-2 rounded-full mb-3 m-0" style={{ background: '#fff2eb', color: '#8c491a' }}>
                {cancelError}
              </p>
            )}
            <button
              onClick={() => cancelMutation.mutate()}
              disabled={cancelMutation.isPending}
              className="w-full py-2.5 rounded-full text-[14px] font-heading transition-colors"
              style={{
                border: '1px solid rgba(198,113,57,.4)',
                background: 'transparent',
                color: '#8c491a',
                cursor: cancelMutation.isPending ? 'not-allowed' : 'pointer',
                opacity: cancelMutation.isPending ? 0.6 : 1,
              }}
            >
              {cancelMutation.isPending ? 'Cancelling…' : 'Cancel order'}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
