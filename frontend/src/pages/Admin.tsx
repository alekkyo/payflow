import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { admin, type InsightsSummary } from '../api/client'

function fmt(cents: number) {
  return `$${(cents / 100).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

function fmtPct(rate: number) {
  return `${(rate * 100).toFixed(1)}%`
}

function timeAgo(iso: string) {
  const mins = Math.floor((Date.now() - new Date(iso).getTime()) / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  return `${hrs}h ago`
}

// ── AI Insights ──────────────────────────────────────────────────────────────

function InsightsSection() {
  const queryClient = useQueryClient()
  const [requested, setRequested] = useState(false)

  const { data, isFetching, error } = useQuery<InsightsSummary>({
    queryKey: ['admin', 'insights'],
    queryFn: () => admin.getInsights(false),
    enabled: requested,
    staleTime: 30 * 60 * 1000,
    refetchInterval: 30 * 60 * 1000,
    retry: false,
  })

  const refresh = useMutation({
    mutationFn: () => admin.getInsights(true),
    onSuccess: (fresh) => queryClient.setQueryData(['admin', 'insights'], fresh),
  })

  const loading = isFetching || refresh.isPending
  const summary = refresh.data ?? data

  return (
    <div style={{ background: '#ebddc5', borderRadius: 28, padding: '22px 24px', boxShadow: '0 1px 2px rgba(46,43,37,.14)' }}>
      <div className="flex items-start justify-between gap-4 mb-4 flex-wrap">
        <div>
          <h4 className="font-heading text-[17px] m-0 mb-1">AI Payment Insights</h4>
          <p className="text-[13px] m-0" style={{ color: 'rgba(32,30,29,.55)' }}>
            Claude-powered summary of the last 7 days. Cached for 30 minutes.
          </p>
        </div>
        {!summary && !loading && (
          <button
            onClick={() => setRequested(true)}
            className="px-4 py-2 rounded-full font-heading text-[13px] transition-colors"
            style={{ background: '#c67139', color: '#f5ead8', border: 'none', cursor: 'pointer' }}
          >
            Generate insights
          </button>
        )}
        {summary && (
          <div className="flex items-center gap-3">
            <span className="text-[12px]" style={{ color: 'rgba(32,30,29,.45)' }}>
              {summary.cached ? 'Cached · ' : ''}{timeAgo(summary.generated_at)}
            </span>
            <button
              onClick={() => refresh.mutate()}
              disabled={loading}
              className="text-[13px] font-heading transition-colors"
              style={{ background: 'transparent', border: 'none', cursor: loading ? 'not-allowed' : 'pointer', color: '#c67139', opacity: loading ? 0.5 : 1 }}
            >
              {loading ? 'Refreshing…' : 'Refresh'}
            </button>
          </div>
        )}
      </div>

      {loading && !summary && (
        <div className="py-6 text-center text-[13px]" style={{ color: 'rgba(32,30,29,.55)', animation: 'pf-pulse 1.4s infinite' }}>
          Analyzing payment data with Claude…
        </div>
      )}

      {error && (
        <div className="px-4 py-3 rounded-[16px] text-[13px]" style={{ background: '#fff2eb', color: '#643312' }}>
          Failed to generate insights. Check that ANTHROPIC_API_KEY is configured.
        </div>
      )}

      {summary && (
        <div className="flex flex-col gap-4">
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(130px, 1fr))', gap: 12 }}>
            {[
              { label: 'Volume (7d)', value: fmt(summary.stats.total_volume_cents) },
              { label: 'Transactions', value: summary.stats.total_transactions.toLocaleString() },
              { label: 'Success rate', value: fmtPct(summary.stats.success_rate) },
              { label: 'Refund rate', value: fmtPct(summary.stats.refund_rate) },
            ].map(({ label, value }) => (
              <div key={label} className="text-center p-3" style={{ background: '#f5ead8', borderRadius: 16 }}>
                <div className="font-heading text-[22px]">{value}</div>
                <div className="text-[11px] mt-0.5" style={{ color: 'rgba(32,30,29,.55)' }}>{label}</div>
              </div>
            ))}
          </div>

          <div
            className="px-5 py-4 text-[14px] leading-relaxed"
            style={{ background: '#f5ead8', borderLeft: '3px solid #c67139', borderRadius: '0 16px 16px 0' }}
          >
            {summary.ai_summary}
          </div>

          {summary.anomalies?.length > 0 && (
            <div className="flex flex-col gap-2">
              {summary.anomalies.map((a, i) => (
                <div
                  key={i}
                  className="flex items-start gap-2 px-4 py-3 text-[13px] rounded-[16px]"
                  style={{ background: '#fff2eb', color: '#643312' }}
                >
                  <span style={{ flexShrink: 0, marginTop: 1 }}>⚠</span>
                  {a}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ── Revenue SVG chart ────────────────────────────────────────────────────────

function RevenueChart({ runs }: { runs: { run_date: string; matched: number }[] }) {
  const W = 560, H = 160, PAD = 10
  if (!runs || runs.length === 0) return (
    <div className="flex items-center justify-center h-[150px] text-[13px]" style={{ color: 'rgba(32,30,29,.4)' }}>
      No data
    </div>
  )

  const vals = runs.map((r) => r.matched)
  const max = Math.max(...vals, 1)
  const pts = vals.map((v, i) => {
    const x = PAD + (i / (vals.length - 1 || 1)) * (W - PAD * 2)
    const y = H - PAD - (v / max) * (H - PAD * 2)
    return `${x},${y}`
  })
  const line = pts.join(' ')
  const area = `M${pts[0]} ${pts.slice(1).map((p) => `L${p}`).join(' ')} L${W - PAD},${H} L${PAD},${H} Z`

  return (
    <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" style={{ width: '100%', height: 150, display: 'block' }}>
      <defs>
        <linearGradient id="revg" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#c67139" stopOpacity="0.28" />
          <stop offset="1" stopColor="#c67139" stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={area} fill="url(#revg)" />
      <polyline points={line} fill="none" stroke="#c67139" strokeWidth="2.75" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

// ── Main Admin component ─────────────────────────────────────────────────────

export function Admin() {
  const queryClient = useQueryClient()
  const [triggerDate, setTriggerDate] = useState('')
  const [triggerMsg, setTriggerMsg] = useState('')

  const { data: runs, isLoading: runsLoading } = useQuery({
    queryKey: ['admin', 'reconciliation'],
    queryFn: () => admin.listReconciliationRuns(),
    refetchInterval: 10_000,
  })

  const { data: queues, isLoading: queuesLoading } = useQuery({
    queryKey: ['admin', 'queues'],
    queryFn: () => admin.getQueueDepths(),
    refetchInterval: 5_000,
  })

  const { data: dlq, isLoading: dlqLoading } = useQuery({
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

  const isLoading = runsLoading && queuesLoading && dlqLoading

  const card = (children: React.ReactNode) => (
    <div style={{ background: '#ebddc5', borderRadius: 28, padding: '22px 24px', boxShadow: '0 1px 2px rgba(46,43,37,.14)' }}>
      {children}
    </div>
  )

  const maxDepth = queues ? Math.max(...Object.values(queues), 1) : 1

  return (
    <div className="max-w-[1180px] mx-auto px-6 pb-20 animate-pf-fade">
      {/* Header */}
      <div className="flex justify-between items-end gap-4 flex-wrap pt-10 pb-6">
        <div>
          <div className="text-[12px] uppercase tracking-[0.1em] mb-1" style={{ color: '#c67139' }}>
            Operations · live
          </div>
          <h1 className="font-heading text-[38px] m-0">Payments dashboard</h1>
        </div>
        <span
          className="inline-flex items-center gap-1.5 text-[12px] px-3 py-1 rounded-full"
          style={{ background: '#f0fae1', color: '#3d472b' }}
        >
          <span
            className="w-[7px] h-[7px] rounded-full"
            style={{ background: '#728157', animation: 'pf-pulse 1.6s infinite' }}
          />
          All systems nominal
        </span>
      </div>

      {/* Loading skeleton */}
      {isLoading ? (
        <div className="flex flex-col gap-4">
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 16 }}>
            {Array.from({ length: 4 }).map((_, i) => <div key={i} className="pf-shim h-[104px]" />)}
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))', gap: 16 }}>
            <div className="pf-shim h-[240px]" />
            <div className="pf-shim h-[240px]" />
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          {/* AI Insights */}
          <InsightsSection />

          {/* Queue depths + Dead-letter side by side */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))', gap: 16 }}>
            {card(
              <>
                <h4 className="font-heading text-[17px] m-0 mb-4">Redis stream depths</h4>
                <p className="text-[13px] m-0 mb-4" style={{ color: 'rgba(32,30,29,.55)' }}>
                  Unprocessed messages per stream. Updated every 5 s.
                </p>
                <div className="flex flex-col gap-4">
                  {queues ? Object.entries(queues).map(([stream, depth]) => {
                    const pct = Math.min((depth / maxDepth) * 100, 100)
                    const backed = depth > 10
                    return (
                      <div key={stream}>
                        <div className="flex justify-between text-[13px] mb-1.5">
                          <span style={{ fontFamily: 'ui-monospace, monospace' }}>
                            {stream.replace('stream:', '')}
                          </span>
                          <span style={{ color: 'rgba(32,30,29,.55)' }}>{depth} queued</span>
                        </div>
                        <div className="h-[7px] rounded-full overflow-hidden" style={{ background: '#dcd3c4' }}>
                          <div
                            className="h-full rounded-full"
                            style={{
                              width: `${pct}%`,
                              background: backed ? '#d67f48' : '#8fa073',
                            }}
                          />
                        </div>
                      </div>
                    )
                  }) : (
                    <div className="pf-shim h-20" />
                  )}
                </div>
              </>
            )}

            {card(
              <>
                <div className="flex justify-between items-center mb-4">
                  <h4 className="font-heading text-[17px] m-0">Dead-letter queue</h4>
                  <span className="text-[11px] px-2.5 py-0.5 rounded-full" style={{ background: '#fff2eb', color: '#643312' }}>
                    {dlq?.count ?? 0} stuck
                  </span>
                </div>
                {!dlq || dlq.count === 0 ? (
                  <div className="px-4 py-3 rounded-[16px] text-[13px]" style={{ background: '#f0fae1', color: '#3d472b' }}>
                    No dead letter messages. All workers are healthy.
                  </div>
                ) : (
                  <div className="flex flex-col gap-3">
                    {dlq.messages.map((msg) => {
                      const error = msg.fields['error'] ?? ''
                      const stream = msg.fields['source_stream'] ?? ''
                      const extras = Object.entries(msg.fields).filter(
                        ([k]) => k !== 'error' && k !== 'source_stream',
                      )
                      return (
                        <div key={msg.id} className="pb-3" style={{ borderBottom: '1px solid rgba(32,30,29,.16)' }}>
                          {/* ID + stream row */}
                          <div className="flex items-center gap-2 mb-1.5 min-w-0">
                            <span
                              className="text-[12px] shrink-0"
                              style={{ fontFamily: 'ui-monospace, monospace', color: 'rgba(32,30,29,.55)' }}
                            >
                              {msg.id}
                            </span>
                            {stream && (
                              <span
                                className="text-[11px] px-2 py-px rounded-full shrink-0"
                                style={{ background: '#f9f4ed', color: '#474238', fontFamily: 'ui-monospace, monospace' }}
                              >
                                {stream}
                              </span>
                            )}
                          </div>
                          {/* Error — truncated to 2 lines, full text on hover */}
                          {error && (
                            <p
                              className="text-[12px] m-0 mb-2"
                              title={error}
                              style={{
                                fontFamily: 'ui-monospace, monospace',
                                color: '#8c491a',
                                display: '-webkit-box',
                                WebkitLineClamp: 2,
                                WebkitBoxOrient: 'vertical',
                                overflow: 'hidden',
                              }}
                            >
                              {error}
                            </p>
                          )}
                          {/* Remaining fields — short values inline, long values clamped */}
                          {extras.length > 0 && (
                            <div className="flex flex-col gap-0.5">
                              {/* Short fields: one flex-wrap row */}
                              {(() => {
                                const short = extras.filter(([, v]) => v.length < 80)
                                const long  = extras.filter(([, v]) => v.length >= 80)
                                return (
                                  <>
                                    {short.length > 0 && (
                                      <div className="flex flex-wrap gap-x-4 gap-y-0.5">
                                        {short.map(([k, v]) => (
                                          <span key={k} className="text-[11px]" style={{ color: 'rgba(32,30,29,.55)' }}>
                                            {k}: <span style={{ fontFamily: 'ui-monospace, monospace' }}>{v}</span>
                                          </span>
                                        ))}
                                      </div>
                                    )}
                                    {long.map(([k, v]) => (
                                      <div key={k} className="text-[11px] mt-0.5" style={{ color: 'rgba(32,30,29,.55)' }}>
                                        <span className="mr-1">{k}:</span>
                                        <span
                                          title={v}
                                          style={{
                                            fontFamily: 'ui-monospace, monospace',
                                            display: '-webkit-box',
                                            WebkitLineClamp: 2,
                                            WebkitBoxOrient: 'vertical',
                                            overflow: 'hidden',
                                            wordBreak: 'break-all',
                                          }}
                                        >
                                          {v}
                                        </span>
                                      </div>
                                    ))}
                                  </>
                                )
                              })()}
                            </div>
                          )}
                        </div>
                      )
                    })}
                  </div>
                )}
              </>
            )}
          </div>

          {/* Charts */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))', gap: 16 }}>
            {card(
              <>
                <div className="flex justify-between items-baseline mb-3.5">
                  <h4 className="font-heading text-[17px] m-0">Reconciliation history</h4>
                  <span className="text-[12px]" style={{ color: 'rgba(32,30,29,.55)' }}>last runs</span>
                </div>
                <RevenueChart runs={runs ?? []} />
              </>
            )}

            {card(
              <>
                <div className="flex justify-between items-baseline mb-3.5">
                  <h4 className="font-heading text-[17px] m-0">Matched payments</h4>
                  <span className="text-[12px]" style={{ color: 'rgba(32,30,29,.55)' }}>per run</span>
                </div>
                {runs && runs.length > 0 ? (
                  <div className="flex items-end gap-1.5 h-[150px]">
                    {runs.slice(-14).map((r) => {
                      const maxM = Math.max(...runs.map((x) => x.matched), 1)
                      const h = Math.max((r.matched / maxM) * 100, 4)
                      return (
                        <div
                          key={r.id}
                          className="flex-1 rounded-t-[6px]"
                          style={{ height: `${h}%`, background: '#aebf92', minHeight: 4 }}
                          title={`${r.run_date}: ${r.matched} matched`}
                        />
                      )
                    })}
                  </div>
                ) : (
                  <div className="flex items-center justify-center h-[150px] text-[13px]" style={{ color: 'rgba(32,30,29,.4)' }}>
                    No data
                  </div>
                )}
              </>
            )}
          </div>

          {/* Reconciliation table */}
          {card(
            <>
              <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
                <div>
                  <h4 className="font-heading text-[17px] m-0 mb-1">Daily reconciliation vs Stripe</h4>
                  <p className="text-[13px] m-0" style={{ color: 'rgba(32,30,29,.55)' }}>
                    Compares local payments against Stripe. Leave date empty to run yesterday.
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <input
                    type="date"
                    value={triggerDate}
                    onChange={(e) => setTriggerDate(e.target.value)}
                    className="rounded-full px-3.5 py-1.5 text-[13px] border"
                    style={{
                      borderColor: 'rgba(32,30,29,.16)',
                      background: '#f5ead8',
                      color: '#201e1d',
                      fontFamily: 'inherit',
                      outline: 'none',
                    }}
                  />
                  <button
                    onClick={() => { setTriggerMsg(''); triggerMutation.mutate() }}
                    disabled={triggerMutation.isPending}
                    className="px-4 py-1.5 rounded-full font-heading text-[13px] transition-colors"
                    style={{
                      background: '#c67139',
                      color: '#f5ead8',
                      border: 'none',
                      cursor: triggerMutation.isPending ? 'not-allowed' : 'pointer',
                      opacity: triggerMutation.isPending ? 0.6 : 1,
                    }}
                  >
                    {triggerMutation.isPending ? 'Triggering…' : 'Trigger run'}
                  </button>
                </div>
              </div>

              {triggerMsg && (
                <div className="text-[13px] px-3 py-2 rounded-[16px] mb-4" style={{ background: '#f5ead8', color: 'rgba(32,30,29,.7)' }}>
                  {triggerMsg}
                </div>
              )}

              <div className="overflow-x-auto">
                <table className="w-full text-[14px] border-collapse">
                  <thead>
                    <tr>
                      {['Date', 'Status', 'Matched', 'Mismatched', 'Missing local', 'Missing Stripe'].map((h) => (
                        <th
                          key={h}
                          className="text-left text-[11px] uppercase tracking-[0.08em] py-2 px-2"
                          style={{ color: 'rgba(32,30,29,.6)', borderBottom: '1px solid rgba(32,30,29,.16)' }}
                        >
                          {h}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {(runs ?? []).map((run) => (
                      <tr key={run.id}>
                        <td className="px-2 py-2 text-[12.5px]" style={{ fontFamily: 'ui-monospace, monospace', borderBottom: '1px solid rgba(32,30,29,.08)' }}>
                          {run.run_date}
                        </td>
                        <td className="px-2 py-2" style={{ borderBottom: '1px solid rgba(32,30,29,.08)' }}>
                          <span
                            className="text-[11px] px-2.5 py-0.5 rounded-full"
                            style={{
                              background: run.status === 'completed' ? '#f0fae1' : run.status === 'failed' ? '#fff2eb' : '#f9f4ed',
                              color: run.status === 'completed' ? '#3d472b' : run.status === 'failed' ? '#643312' : '#474238',
                            }}
                          >
                            {run.status}
                          </span>
                        </td>
                        <td className="px-2 py-2 font-medium text-right" style={{ color: '#56633f', borderBottom: '1px solid rgba(32,30,29,.08)' }}>{run.matched}</td>
                        <td className="px-2 py-2 font-medium text-right" style={{ color: '#8c491a', borderBottom: '1px solid rgba(32,30,29,.08)' }}>{run.mismatched}</td>
                        <td className="px-2 py-2 font-medium text-right" style={{ color: '#8c491a', borderBottom: '1px solid rgba(32,30,29,.08)' }}>{run.missing_local}</td>
                        <td className="px-2 py-2 font-medium text-right" style={{ color: '#8c491a', borderBottom: '1px solid rgba(32,30,29,.08)' }}>{run.missing_stripe}</td>
                      </tr>
                    ))}
                    {(!runs || runs.length === 0) && (
                      <tr>
                        <td colSpan={6} className="px-2 py-10 text-center text-[14px]" style={{ color: 'rgba(32,30,29,.4)' }}>
                          No reconciliation runs yet. Trigger one above.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}
