import { useOrderStream, ORDER_STATUS_LABELS, TERMINAL_STATUSES } from '../hooks/useOrderStream'

const SAGA_STEPS = [
  'created',
  'inventory_reserved',
  'payment_processing',
  'payment_captured',
  'confirmed',
  'fulfilled',
]

const FAILURE_STATUSES = new Set(['payment_failed', 'cancelled'])

type Props = {
  orderId: string
  initialStatus: string
}

export function OrderStatusTracker({ orderId, initialStatus }: Props) {
  const status = useOrderStream(orderId, initialStatus)
  const label = ORDER_STATUS_LABELS[status] ?? status
  const isFailed = FAILURE_STATUSES.has(status)
  const isTerminal = TERMINAL_STATUSES.has(status)

  return (
    <div className="bg-white border rounded-lg p-6">
      <div className="flex items-center justify-between mb-6">
        <h3 className="font-semibold text-gray-800">Order Status</h3>
        <span className={`text-sm font-medium px-3 py-1 rounded-full ${
          isFailed
            ? 'bg-red-100 text-red-700'
            : isTerminal
            ? 'bg-green-100 text-green-700'
            : 'bg-blue-100 text-blue-700'
        }`}>
          {label}
        </span>
      </div>

      {!isFailed && (
        <div className="relative">
          {/* Progress line */}
          <div className="absolute left-3 top-3 bottom-3 w-0.5 bg-gray-200" />

          <div className="space-y-4">
            {SAGA_STEPS.map((step) => {
              const stepIndex = SAGA_STEPS.indexOf(step)
              const currentIndex = SAGA_STEPS.indexOf(status)
              const done = currentIndex > stepIndex
              const active = currentIndex === stepIndex

              return (
                <div key={step} className="flex items-center gap-4 relative">
                  <div className={`w-6 h-6 rounded-full border-2 flex items-center justify-center z-10 flex-shrink-0 ${
                    done
                      ? 'bg-green-500 border-green-500'
                      : active
                      ? 'bg-blue-500 border-blue-500 animate-pulse'
                      : 'bg-white border-gray-300'
                  }`}>
                    {done && (
                      <svg className="w-3 h-3 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M5 13l4 4L19 7" />
                      </svg>
                    )}
                  </div>
                  <span className={`text-sm ${
                    done ? 'text-gray-800' : active ? 'text-blue-700 font-medium' : 'text-gray-400'
                  }`}>
                    {ORDER_STATUS_LABELS[step] ?? step}
                  </span>
                  {active && !isTerminal && (
                    <span className="text-xs text-blue-500 animate-pulse">In progress...</span>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      )}

      {isFailed && (
        <p className="text-sm text-red-600 mt-2">
          {status === 'payment_failed'
            ? 'Your payment could not be processed. Any reserved inventory has been released.'
            : 'This order has been cancelled.'}
        </p>
      )}
    </div>
  )
}
