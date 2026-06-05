import { Link, useNavigate } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'

export function Navbar() {
  const { user, logout, isAdmin } = useAuth()
  const navigate = useNavigate()

  const handleLogout = async () => {
    await logout()
    navigate('/login')
  }

  return (
    <nav className="bg-gray-900 text-white px-6 py-4 flex items-center justify-between">
      <Link to="/" className="text-xl font-bold tracking-tight">
        PayFlow
      </Link>

      <div className="flex items-center gap-6 text-sm">
        <Link to="/" className="hover:text-gray-300">Products</Link>

        {user ? (
          <>
            <Link to="/orders" className="hover:text-gray-300">My Orders</Link>
            {isAdmin && (
              <Link to="/admin" className="hover:text-gray-300 text-yellow-400">Admin</Link>
            )}
            <span className="text-gray-400">{user.email}</span>
            <button
              onClick={handleLogout}
              className="bg-gray-700 hover:bg-gray-600 px-3 py-1 rounded"
            >
              Logout
            </button>
          </>
        ) : (
          <Link
            to="/login"
            className="bg-indigo-600 hover:bg-indigo-500 px-3 py-1 rounded"
          >
            Sign in
          </Link>
        )}
      </div>
    </nav>
  )
}
