import { useEffect, useState } from 'react'

// ORDER_STATUS_LABELS maps backend status strings to human-readable display text.
// The order matches the happy-path saga progression.
export const ORDER_STATUS_LABELS: Record<string, string> = {
  created:             'Order placed',
  inventory_reserved:  'Inventory reserved',
  payment_processing:  'Processing payment',
  payment_captured:    'Payment confirmed',
  confirmed:           'Order confirmed',
  fulfilled:           'Fulfilled',
  cancelled:           'Cancelled',
  payment_failed:      'Payment failed',
  refunded:            'Refunded',
}

// confirmed is intentionally excluded — the saga still transitions to fulfilled.
// Only fulfilled (and failure/cancellation states) are true end states.
export const TERMINAL_STATUSES = new Set([
  'fulfilled',
  'cancelled',
  'payment_failed',
  'refunded',
])

// useOrderStream subscribes to the SSE endpoint for a given order and returns
// the latest status string. The connection is closed automatically when the
// status reaches a terminal state or the component unmounts.
//
// SSE (Server-Sent Events) is a simple HTTP streaming protocol — the server
// holds the connection open and pushes newline-delimited "data: <value>" events.
// Unlike WebSockets, SSE is one-directional (server → client) and works over
// plain HTTP/1.1 with no special handshake. Perfect for order status tracking.
export function useOrderStream(orderId: string | null, initialStatus?: string) {
  const [status, setStatus] = useState(initialStatus ?? 'created')

  useEffect(() => {
    if (!orderId) return
    if (TERMINAL_STATUSES.has(status)) return

    const token = localStorage.getItem('token')
    if (!token) return

    // EventSource doesn't support custom headers, so we pass the token as a
    // query param. The backend Auth middleware checks both the Authorization
    // header and the token query parameter.
    const url = `/api/orders/${orderId}/events/stream?token=${token}`
    const es = new EventSource(url)

    // The backend sends named events: `event: status\ndata: {"order_id":"...","status":"..."}`.
    // es.onmessage only fires for unnamed ("message") events, so we must use addEventListener.
    es.addEventListener('status', (e) => {
      try {
        const { status: newStatus } = JSON.parse(e.data) as { status: string }
        if (newStatus) {
          setStatus(newStatus)
          if (TERMINAL_STATUSES.has(newStatus)) {
            es.close()
          }
        }
      } catch {
        // malformed payload — ignore
      }
    })

    // Server signals the stream is finished after a terminal status.
    es.addEventListener('done', () => es.close())

    es.onerror = () => {
      es.close()
    }

    return () => es.close()
  }, [orderId, status])

  return status
}
