import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { ApiError } from '../api/client'

const DEMO = [
  { label: 'Admin', email: 'admin@payflow.dev', password: 'demo-admin-123' },
  { label: 'Customer', email: 'customer@payflow.dev', password: 'demo-customer-123' },
]

function CheckIcon() {
  return (
    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.75" strokeLinecap="round" strokeLinejoin="round">
      <path d="M20 6 9 17l-5-5" />
    </svg>
  )
}

export function Login() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const { login } = useAuth()
  const navigate = useNavigate()

  const canSubmit = email.trim().length > 0 && password.length > 0

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!canSubmit) return
    setError('')
    setLoading(true)
    try {
      await login(email, password)
      navigate('/')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Something went wrong')
    } finally {
      setLoading(false)
    }
  }

  const fill = (d: typeof DEMO[number]) => {
    setEmail(d.email)
    setPassword(d.password)
    setError('')
  }

  return (
    <div className="min-h-screen flex items-center justify-center px-6 py-12" style={{ background: '#f5ead8' }}>
      <div
        className="w-full animate-pf-fade"
        style={{
          maxWidth: 880,
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))',
          background: '#ebddc5',
          borderRadius: 28,
          overflow: 'hidden',
          boxShadow: '0 12px 32px rgba(46,43,37,.22)',
        }}
      >
        {/* Left panel — sage */}
        <div
          className="flex flex-col gap-4"
          style={{ background: '#728157', color: '#f5ead8', padding: '42px 38px' }}
        >
          <div className="flex items-center gap-2.5 font-heading text-[22px]">
            <span
              className="w-[29px] h-[29px] rounded-full flex items-center justify-center text-[18px] font-heading leading-none"
              style={{
                background: '#f5ead8',
                color: '#56633f',
                boxShadow: 'inset 0 0 0 2px rgba(86,99,63,.4)',
              }}
            >
              $
            </span>
            PayFlow
          </div>
          <h2 className="font-heading text-[30px] mt-2.5 mb-0 leading-[1.1]" style={{ color: '#f5ead8' }}>
            Payments that just work.
          </h2>
          <p className="text-[14px] m-0" style={{ opacity: 0.9 }}>
            A demo storefront on a production-grade Go saga — idempotent, reconciled and fully observable.
          </p>
          <div className="flex flex-col gap-3 mt-auto pt-5">
            {[
              'Distributed saga orchestration',
              'Idempotent payments & webhooks',
              'Real-time order tracking via SSE',
            ].map((feat) => (
              <div key={feat} className="flex items-center gap-2.5 text-[13.5px]">
                <CheckIcon />
                {feat}
              </div>
            ))}
          </div>
        </div>

        {/* Right panel — form */}
        <div className="flex flex-col gap-4" style={{ padding: '42px 38px' }}>
          <div>
            <div
              className="text-[11px] uppercase tracking-[0.1em]"
              style={{ color: '#c67139' }}
            >
              Sign in · test mode
            </div>
            <h3 className="font-heading text-[25px] mt-1.5 mb-0">Welcome back</h3>
          </div>

          <form onSubmit={handleSubmit} className="flex flex-col gap-4">
            <div className="flex flex-col gap-1">
              <label className="text-[12px]" style={{ color: 'rgba(32,30,29,.7)' }}>Email</label>
              <input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="you@example.com"
                className="w-full min-h-9 px-3.5 py-1.5 text-[14px] rounded-full border"
                style={{
                  background: '#f5ead8',
                  color: '#201e1d',
                  borderColor: 'rgba(32,30,29,.16)',
                  caretColor: '#c67139',
                  outline: 'none',
                  fontFamily: 'inherit',
                }}
              />
            </div>

            <div className="flex flex-col gap-1">
              <label className="text-[12px]" style={{ color: 'rgba(32,30,29,.7)' }}>Password</label>
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="••••••••"
                className="w-full min-h-9 px-3.5 py-1.5 text-[14px] rounded-full border"
                style={{
                  background: '#f5ead8',
                  color: '#201e1d',
                  borderColor: 'rgba(32,30,29,.16)',
                  caretColor: '#c67139',
                  outline: 'none',
                  fontFamily: 'inherit',
                }}
              />
            </div>

            {error && (
              <p className="text-[13px] text-pf-accent-700 bg-pf-accent-100 px-3 py-2 rounded-full m-0">
                {error}
              </p>
            )}

            <button
              type="submit"
              disabled={!canSubmit || loading}
              className="w-full mt-1 py-3 rounded-full font-heading text-[15px] transition-colors"
              style={{
                background: canSubmit && !loading ? '#c67139' : '#c67139',
                color: '#f5ead8',
                opacity: !canSubmit || loading ? 0.45 : 1,
                cursor: !canSubmit || loading ? 'not-allowed' : 'pointer',
                border: 'none',
              }}
            >
              {loading ? 'Signing in…' : 'Sign in'}
            </button>
          </form>

          <div className="text-[12px] text-center mt-0.5" style={{ color: 'rgba(32,30,29,.55)' }}>
            or use a demo account
          </div>

          <div className="flex gap-2.5">
            {DEMO.map((d) => (
              <button
                key={d.email}
                type="button"
                onClick={() => fill(d)}
                className="flex-1 flex flex-col items-center gap-px py-3 rounded-full font-heading text-[13px] transition-colors"
                style={{
                  background: 'transparent',
                  border: '1px solid rgba(32,30,29,.16)',
                  color: '#201e1d',
                  cursor: 'pointer',
                }}
              >
                <span>{d.label}</span>
                <span
                  className="text-[10px]"
                  style={{ fontFamily: 'ui-monospace, monospace', color: 'rgba(32,30,29,.55)' }}
                >
                  {d.email}
                </span>
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
