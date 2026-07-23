import { Link, useNavigate, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { useCartContext } from '../context/CartContext'

const ACCENT = '#c67139'
const TEXT   = '#201e1d'
const BG     = '#f5ead8'

export function Navbar() {
  const { user, logout, isAdmin } = useAuth()
  const { items } = useCartContext()
  const navigate = useNavigate()
  const { pathname } = useLocation()

  const cartCount = items.reduce((s, i) => s + i.quantity, 0)

  const handleLogout = async () => {
    await logout()
    navigate('/login')
  }

  const isActive = (path: string) =>
    path === '/' ? pathname === '/' : pathname.startsWith(path)

  const linkStyle = (path: string): React.CSSProperties => ({
    fontSize: 14,
    fontFamily: 'Figtree, system-ui, sans-serif',
    textDecoration: 'none',
    color: isActive(path) ? ACCENT : TEXT,
  })

  return (
    <nav
      style={{
        position: 'sticky',
        top: 0,
        zIndex: 20,
        display: 'flex',
        alignItems: 'center',
        flexWrap: 'wrap',
        gap: '12px 20px',
        padding: '16px 24px',
        background: 'rgba(245,234,216,.88)',
        backdropFilter: 'blur(10px)',
        WebkitBackdropFilter: 'blur(10px)',
        borderBottom: '1px solid rgba(32,30,29,.16)',
      }}
    >
      {/* Logo */}
      <Link
        to="/"
        style={{
          fontFamily: 'Caprasimo, system-ui, sans-serif',
          fontSize: 18,
          marginRight: 'auto',
          display: 'inline-flex',
          alignItems: 'center',
          gap: 10,
          textDecoration: 'none',
          color: TEXT,
        }}
      >
        <span
          style={{
            width: 27,
            height: 27,
            borderRadius: '999px',
            background: ACCENT,
            color: BG,
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontFamily: 'Caprasimo, system-ui, sans-serif',
            fontSize: 16,
            lineHeight: 1,
            flexShrink: 0,
            boxShadow: `inset 0 0 0 2px rgba(245,234,216,.55)`,
          }}
        >
          $
        </span>
        PayFlow
      </Link>

      {/* Nav links */}
      <Link to="/" style={linkStyle('/')}>Shop</Link>

      {user && <Link to="/orders" style={linkStyle('/orders')}>Orders</Link>}
      {user && isAdmin && <Link to="/admin" style={linkStyle('/admin')}>Admin</Link>}

      {/* GitHub */}
      <a
        href="https://github.com/alekkyo/payflow"
        target="_blank"
        rel="noopener"
        aria-label="View source on GitHub"
        style={{
          width: 36,
          height: 36,
          borderRadius: '999px',
          border: '1px solid rgba(32,30,29,.16)',
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: TEXT,
          textDecoration: 'none',
          flexShrink: 0,
        }}
      >
        <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
          <path d="M12 .5C5.4.5 0 5.9 0 12.6c0 5.3 3.4 9.8 8.2 11.4.6.1.8-.3.8-.6v-2c-3.3.7-4-1.6-4-1.6-.5-1.4-1.3-1.8-1.3-1.8-1.1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1.1 1.8 2.8 1.3 3.5 1 .1-.8.4-1.3.8-1.6-2.7-.3-5.5-1.3-5.5-5.9 0-1.3.5-2.4 1.2-3.2-.1-.4-.5-1.6.2-3.3 0 0 1-.3 3.3 1.2a11.5 11.5 0 0 1 6 0C18 4.5 19 4.8 19 4.8c.7 1.7.2 2.9.1 3.3.8.8 1.2 1.9 1.2 3.2 0 4.6-2.8 5.6-5.5 5.9.4.4.8 1.1.8 2.3v3.3c0 .3.2.7.8.6A12 12 0 0 0 24 12.6C24 5.9 18.6.5 12 .5z" />
        </svg>
      </a>

      {/* Cart */}
      {user && (
        <Link
          to="/cart"
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 8,
            padding: '7px 14px',
            borderRadius: '999px',
            border: '1px solid rgba(32,30,29,.16)',
            fontFamily: 'Caprasimo, system-ui, sans-serif',
            fontSize: 14,
            color: TEXT,
            textDecoration: 'none',
            flexShrink: 0,
          }}
        >
          <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" strokeWidth="2.75" strokeLinecap="round" strokeLinejoin="round">
            <path d="M6 2 3 6v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V6l-3-4z" />
            <path d="M3 6h18" />
            <path d="M16 10a4 4 0 0 1-8 0" />
          </svg>
          Cart
          <span
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              minWidth: 20,
              height: 20,
              padding: '0 5px',
              background: ACCENT,
              color: BG,
              borderRadius: '999px',
              fontSize: 11,
              fontFamily: 'Caprasimo, system-ui, sans-serif',
            }}
          >
            {cartCount}
          </span>
        </Link>
      )}

      {/* Sign out / Sign in */}
      {user ? (
        <button
          onClick={handleLogout}
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            background: 'transparent',
            border: 'none',
            cursor: 'pointer',
            color: ACCENT,
            fontFamily: 'Caprasimo, system-ui, sans-serif',
            fontSize: 14,
            padding: '4px 8px',
            borderRadius: '999px',
          }}
        >
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.75" strokeLinecap="round" strokeLinejoin="round">
            <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
            <path d="m16 17 5-5-5-5" />
            <path d="M21 12H9" />
          </svg>
          Sign out
        </button>
      ) : (
        <Link
          to="/login"
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            padding: '7px 16px',
            borderRadius: '999px',
            background: ACCENT,
            color: BG,
            fontFamily: 'Caprasimo, system-ui, sans-serif',
            fontSize: 14,
            textDecoration: 'none',
          }}
        >
          Sign in
        </Link>
      )}
    </nav>
  )
}
