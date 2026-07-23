import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { orders } from '../api/client'
import { ORDER_STATUS_LABELS } from '../hooks/useOrderStream'

function fmt(cents: number) {
  return `$${Math.round(cents / 100)}`
}

function fmtDate(iso: string) {
  return new Date(iso).toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' })
}

function StatusTag({ status }: { status: string }) {
  const isFailed = status === 'payment_failed' || status === 'cancelled'
  const isDone = status === 'fulfilled' || status === 'refunded'
  const bg = isFailed ? '#fff2eb' : isDone ? '#f0fae1' : '#f9f4ed'
  const color = isFailed ? '#643312' : isDone ? '#3d472b' : '#474238'
  return (
    <span className="text-[11px] px-2.5 py-0.5 rounded-full" style={{ background: bg, color }}>
      {ORDER_STATUS_LABELS[status] ?? status}
    </span>
  )
}

export function OrderList() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['orders'],
    queryFn: () => orders.list(),
  })

  if (isLoading) {
    return (
      <div className="max-w-[1180px] mx-auto px-6 pb-20">
        <div className="pt-10 pb-6">
          <div className="pf-shim h-10 w-48" />
        </div>
        <div className="flex flex-col gap-3.5">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="pf-shim h-20" style={{ borderRadius: 28 }} />
          ))}
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="max-w-[1180px] mx-auto px-6 py-16 text-center" style={{ color: '#8c491a' }}>
        Failed to load orders.
      </div>
    )
  }

  const orderList = data?.orders ?? []

  return (
    <div className="max-w-[1180px] mx-auto px-6 pb-20 animate-pf-fade">
      <div className="pt-10 pb-6">
        <h1 className="font-heading text-[38px] m-0">Your orders</h1>
      </div>

      {orderList.length === 0 ? (
        <div className="text-center py-24">
          <div className="font-heading text-[22px] mb-1.5">No orders yet</div>
          <p className="m-0 mb-5" style={{ color: 'rgba(32,30,29,.55)' }}>Place one and watch the saga run live.</p>
          <Link
            to="/"
            className="inline-flex items-center px-5 py-3 rounded-full font-heading text-[14px] no-underline transition-colors"
            style={{ background: '#c67139', color: '#f5ead8' }}
          >
            Browse products
          </Link>
        </div>
      ) : (
        <>
          <h3 className="font-heading text-[20px] mt-0 mb-3.5">Order history</h3>
          <div className="flex flex-col gap-3.5">
            {orderList.map((order) => (
              <Link
                key={order.id}
                to={`/orders/${order.id}`}
                className="block no-underline transition-shadow"
                style={{
                  background: '#ebddc5',
                  borderRadius: 28,
                  padding: '18px 20px',
                  boxShadow: '0 1px 2px rgba(46,43,37,.14)',
                  color: '#201e1d',
                }}
              >
                <div className="flex justify-between items-center gap-3.5 flex-wrap">
                  <div>
                    <div className="flex items-center gap-2.5 flex-wrap">
                      <span className="font-heading text-[17px]">
                        Order #{order.id.slice(0, 8).toUpperCase()}
                      </span>
                      <StatusTag status={order.status} />
                    </div>
                    <div className="text-[13px] mt-0.5" style={{ color: 'rgba(32,30,29,.55)' }}>
                      {order.items?.length ?? 0} item{(order.items?.length ?? 0) !== 1 ? 's' : ''} · {fmtDate(order.created_at)}
                    </div>
                  </div>
                  <div className="font-heading text-[20px]">
                    {fmt(order.total_cents)}
                  </div>
                </div>

                {order.status === 'payment_failed' && (
                  <div
                    className="flex items-center gap-2.5 mt-3.5 px-3.5 py-3 rounded-[16px] text-[13px]"
                    style={{ background: '#fff2eb', color: '#643312' }}
                  >
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.75" strokeLinecap="round" strokeLinejoin="round" style={{ flexShrink: 0 }}>
                      <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" />
                      <path d="M12 9v4" />
                      <path d="M12 17h.01" />
                    </svg>
                    Payment failed — click to view details and retry
                  </div>
                )}
              </Link>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
