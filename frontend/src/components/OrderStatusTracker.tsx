import { useEffect, useRef, useState } from 'react'
import { useOrderStream, ORDER_STATUS_LABELS, TERMINAL_STATUSES } from '../hooks/useOrderStream'

const SAGA_STEPS = [
  { key: 'created',             label: 'Order placed',         desc: 'Your order has been received.' },
  { key: 'inventory_reserved',  label: 'Inventory reserved',   desc: 'Stock is held for your items.' },
  { key: 'payment_processing',  label: 'Payment processing',   desc: 'Charge is being authorized.' },
  { key: 'payment_captured',    label: 'Payment captured',     desc: 'Funds confirmed.' },
  { key: 'confirmed',           label: 'Order confirmed',      desc: 'Fulfilment in progress.' },
  { key: 'fulfilled',           label: 'Fulfilled',            desc: 'Your order is on its way.' },
]

const FAILURE_STATUSES = new Set(['payment_failed', 'cancelled'])

type Props = {
  orderId: string
  initialStatus: string
  onStatusChange?: (status: string) => void
}

export function OrderStatusTracker({ orderId, initialStatus, onStatusChange }: Props) {
  const status = useOrderStream(orderId, initialStatus)
  const [timestamps, setTimestamps] = useState<Record<string, string>>({})
  const [connecting, setConnecting] = useState(!TERMINAL_STATUSES.has(initialStatus))

  const isFailed = FAILURE_STATUSES.has(status)
  const isTerminal = TERMINAL_STATUSES.has(status)

  const prevRef = useRef(initialStatus)
  useEffect(() => {
    if (status !== prevRef.current) {
      prevRef.current = status
      setConnecting(false)
      setTimestamps((t) => ({ ...t, [status]: new Date().toISOString() }))
      onStatusChange?.(status)
    }
  }, [status, onStatusChange])

  useEffect(() => {
    if (!connecting) return
    const t = setTimeout(() => setConnecting(false), 2000)
    return () => clearTimeout(t)
  }, [connecting])

  const currentIndex = SAGA_STEPS.findIndex((s) => s.key === status)

  return (
    <div>
      {/* Header */}
      <div
        className="flex justify-between items-start gap-4 flex-wrap px-6 py-5"
        style={{ borderBottom: '1px solid rgba(32,30,29,.16)' }}
      >
        <div>
          <div className="flex items-center gap-2.5 flex-wrap">
            <span className="font-heading text-[22px]">Order status</span>
            <span
              className="text-[11px] px-2.5 py-0.5 rounded-full"
              style={{
                background: isFailed ? '#fff2eb' : isTerminal ? '#f0fae1' : '#fff2eb',
                color: isFailed ? '#643312' : isTerminal ? '#3d472b' : '#8c491a',
              }}
            >
              {ORDER_STATUS_LABELS[status] ?? status}
            </span>
          </div>
        </div>
        <div className="text-right">
          {!isTerminal && !isFailed && (
            <div className="inline-flex items-center gap-1.5 text-[12px]" style={{ color: '#8c491a' }}>
              <span
                className="w-2 h-2 rounded-full bg-pf-accent"
                style={{ animation: 'pf-pulse 1.4s infinite' }}
              />
              Live · streaming via SSE
            </div>
          )}
        </div>
      </div>

      {/* Timeline */}
      <div className="p-7">
        {connecting ? (
          <div>
            <div className="flex items-center gap-2.5 text-[13px] mb-4" style={{ color: 'rgba(32,30,29,.6)' }}>
              <span
                className="w-3.5 h-3.5 rounded-full border-2 border-r-transparent"
                style={{ borderColor: '#c67139', borderRightColor: 'transparent', animation: 'pf-spin .7s linear infinite' }}
              />
              Connecting to live stream…
            </div>
            <div className="flex flex-col gap-3.5">
              {[52, 64, 44].map((w, i) => (
                <div key={i} className="pf-shim h-4" style={{ width: `${w}%` }} />
              ))}
            </div>
          </div>
        ) : isFailed ? (
          <div
            className="flex items-start gap-3 p-4 rounded-[16px] text-[13px]"
            style={{ background: '#fff2eb', color: '#643312' }}
          >
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2.75" strokeLinecap="round" strokeLinejoin="round" style={{ flexShrink: 0, marginTop: 1 }}>
              <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" />
              <path d="M12 9v4" />
              <path d="M12 17h.01" />
            </svg>
            {status === 'payment_failed'
              ? 'Payment could not be processed. Any reserved inventory has been released.'
              : 'This order has been cancelled.'}
          </div>
        ) : (
          <div>
            {SAGA_STEPS.map((step, i) => {
              const done = currentIndex > i
              const active = currentIndex === i && !isTerminal
              const completed = currentIndex === i && isTerminal && i === SAGA_STEPS.length - 1
              const dotDone = done || (currentIndex === i && isTerminal)
              const pending = currentIndex < i
              const showLine = i < SAGA_STEPS.length - 1
              const lineColored = currentIndex > i
              const ts = timestamps[step.key]

              return (
                <div key={step.key} style={{ display: 'grid', gridTemplateColumns: '34px 1fr', gap: 16 }}>
                  <div className="flex flex-col items-center">
                    {/* Dot */}
                    {dotDone || completed ? (
                      <div
                        className="w-[26px] h-[26px] rounded-full flex items-center justify-center flex-none"
                        style={{ background: '#8fa073', color: '#fff' }}
                      >
                        <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="3.2" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M20 6 9 17l-5-5" />
                        </svg>
                      </div>
                    ) : active ? (
                      <div
                        className="w-[26px] h-[26px] rounded-full flex items-center justify-center flex-none"
                        style={{
                          background: '#fff2eb',
                          border: '2px solid #c67139',
                          animation: 'pf-pulse 1.4s infinite',
                        }}
                      >
                        <span
                          className="w-[9px] h-[9px] rounded-full border-2 border-r-transparent"
                          style={{ borderColor: '#c67139', borderRightColor: 'transparent', animation: 'pf-spin .7s linear infinite' }}
                        />
                      </div>
                    ) : pending ? (
                      <div
                        className="w-[26px] h-[26px] rounded-full flex-none"
                        style={{ background: '#f5ead8', border: '2px solid #c0b6a5' }}
                      />
                    ) : null}

                    {/* Connector line */}
                    {showLine && (
                      <div
                        className="w-0.5 flex-1 my-1"
                        style={{ minHeight: 20, background: lineColored ? '#8fa073' : '#dcd3c4' }}
                      />
                    )}
                  </div>

                  <div className="pb-5">
                    <div
                      className="font-heading text-[16px]"
                      style={{ color: pending ? 'rgba(32,30,29,.45)' : '#201e1d' }}
                    >
                      {step.label}
                    </div>
                    <div className="text-[13px]" style={{ color: 'rgba(32,30,29,.55)' }}>
                      {step.desc}
                    </div>
                    {ts && (
                      <div
                        className="text-[11px] mt-0.5"
                        style={{ fontFamily: 'ui-monospace, monospace', color: 'rgba(32,30,29,.45)' }}
                      >
                        {new Date(ts).toLocaleTimeString()}
                      </div>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
