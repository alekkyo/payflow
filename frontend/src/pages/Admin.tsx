import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { admin } from '../api/client'

const RUN_STATUS_COLORS: Record<string, string> = {
  completed: 'bg-green-100 text-green-700',
  running: 'bg-blue-100 text-blue-700',
  failed: 'bg-red-100 text-red-700',
}

export function Admin() {
  const queryClient = useQueryClient()
  const [triggerDate, setTriggerDate] = useState('')
  const [triggerMsg, setTriggerMsg] = useState('')

  // Auto-refresh so the dashboard stays current without manual reloading.
  const { data: runs } = useQuery({
    queryKey: ['admin', 'reconciliation'],
    queryFn: () => admin.listReconciliationRuns(),
    refetchInterval: 10_000,
  })

  const { data: queues } = useQuery({
    queryKey: ['admin', 'queues'],
    queryFn: () => admin.getQueueDepths(),
    refetchInterval: 5_000,
  })

  const { data: dlq } = useQuery({
    queryKey: ['admin', 'dlq'],
    queryFn: () => admin.listDeadLetterMessages(),
    refetchInterval: 30_000,
  })

  const triggerMutation = useMutation({
    mutationFn: () => admin.triggerReconciliation(triggerDate || undefined),
    onSuccess: (res) => {
      setTriggerMsg(res.message)
      queryClient.invalidateQueries({ queryKey: ['admin', 'reconciliation'] })
    },
    onError: () => setTriggerMsg('Failed to trigger reconciliation'),
  })

  return (
    <div className="max-w-5xl mx-auto px-6 py-8 space-y-10">
      <h1 className="text-2xl font-bold text-gray-900">Admin Dashboard</h1>

      {/* ── Queue depths ──────────────────────────────────────── */}
      <section>
        <h2 className="text-lg font-semibold text-gray-800 mb-3">Queue Depths</h2>
        <p className="text-sm text-gray-500 mb-4">
          Number of unprocessed messages in each Redis Stream. Updated every 5 s.
        </p>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
          {queues ? (
            Object.entries(queues).map(([stream, depth]) => (
              <div key={stream} className="bg-white border rounded-xl p-4 text-center">
                <p className={`text-3xl font-bold ${depth > 0 ? 'text-indigo-600' : 'text-gray-900'}`}>
                  {depth}
                </p>
                <p className="text-xs text-gray-500 mt-1 truncate" title={stream}>
                  {stream.replace('stream:', '')}
                </p>
              </div>
            ))
          ) : (
            <p className="text-sm text-gray-400">Loading...</p>
          )}
        </div>
      </section>

      {/* ── Reconciliation ────────────────────────────────────── */}
      <section>
        <div className="flex flex-wrap items-center justify-between gap-3 mb-3">
          <div>
            <h2 className="text-lg font-semibold text-gray-800">Reconciliation</h2>
            <p className="text-sm text-gray-500">
              Compares local payments against Stripe. Leave date empty to run yesterday.
            </p>
          </div>
          <div className="flex items-center gap-2">
            <input
              type="date"
              value={triggerDate}
              onChange={(e) => setTriggerDate(e.target.value)}
              className="border rounded-lg px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
            />
            <button
              onClick={() => { setTriggerMsg(''); triggerMutation.mutate() }}
              disabled={triggerMutation.isPending}
              className="bg-indigo-600 hover:bg-indigo-500 text-white px-4 py-1.5 rounded-lg text-sm font-medium disabled:opacity-50 transition-colors"
            >
              {triggerMutation.isPending ? 'Triggering...' : 'Trigger run'}
            </button>
          </div>
        </div>

        {triggerMsg && (
          <p className="text-sm text-gray-600 bg-gray-50 px-3 py-2 rounded-lg mb-3">{triggerMsg}</p>
        )}

        <div className="bg-white border rounded-xl overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-gray-50 border-b">
              <tr>
                <th className="px-4 py-3 text-left font-medium text-gray-600">Date</th>
                <th className="px-4 py-3 text-left font-medium text-gray-600">Status</th>
                <th className="px-4 py-3 text-right font-medium text-gray-600">Matched</th>
                <th className="px-4 py-3 text-right font-medium text-gray-600">Mismatched</th>
                <th className="px-4 py-3 text-right font-medium text-gray-600">Missing local</th>
                <th className="px-4 py-3 text-right font-medium text-gray-600">Missing Stripe</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {(runs ?? []).map((run) => (
                <tr key={run.id} className="hover:bg-gray-50">
                  <td className="px-4 py-3 font-mono text-xs">{run.run_date}</td>
                  <td className="px-4 py-3">
                    <span
                      className={`text-xs font-medium px-2 py-0.5 rounded-full ${
                        RUN_STATUS_COLORS[run.status] ?? 'bg-gray-100 text-gray-700'
                      }`}
                    >
                      {run.status}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-right text-green-700 font-medium">{run.matched}</td>
                  <td className="px-4 py-3 text-right text-yellow-700 font-medium">{run.mismatched}</td>
                  <td className="px-4 py-3 text-right text-red-700 font-medium">{run.missing_local}</td>
                  <td className="px-4 py-3 text-right text-red-700 font-medium">{run.missing_stripe}</td>
                </tr>
              ))}
              {(!runs || runs.length === 0) && (
                <tr>
                  <td colSpan={6} className="px-4 py-8 text-center text-gray-400">
                    No reconciliation runs yet. Trigger one above.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

      {/* ── Dead letter queue ─────────────────────────────────── */}
      <section>
        <h2 className="text-lg font-semibold text-gray-800 mb-1">
          Dead Letter Queue
          {dlq && dlq.count > 0 && (
            <span className="ml-2 text-sm font-normal text-red-600">({dlq.count} messages)</span>
          )}
        </h2>
        <p className="text-sm text-gray-500 mb-4">
          Messages that failed processing after all retries. Require manual investigation.
        </p>

        {!dlq || dlq.count === 0 ? (
          <p className="text-sm text-green-700 bg-green-50 border border-green-200 px-4 py-3 rounded-xl">
            No dead letter messages. All workers are healthy.
          </p>
        ) : (
          <div className="space-y-3">
            {dlq.messages.map((msg) => (
              <div key={msg.id} className="bg-white border border-red-200 rounded-xl p-4">
                <p className="text-xs font-mono text-gray-400 mb-3">{msg.id}</p>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-1.5">
                  {Object.entries(msg.fields).map(([k, v]) => (
                    <div key={k} className="flex gap-2 text-xs">
                      <span className="text-gray-500 shrink-0 w-28">{k}:</span>
                      <span className="font-mono text-gray-800 truncate">{v}</span>
                    </div>
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  )
}
