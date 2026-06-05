import type { ReactNode } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { Navbar } from './components/Navbar'
import { Login } from './pages/Login'
import { ProductCatalog } from './pages/ProductCatalog'
import { Cart } from './pages/Cart'
import { OrderList } from './pages/OrderList'
import { OrderDetail } from './pages/OrderDetail'
import { Admin } from './pages/Admin'
import { AuthProvider, useAuth } from './context/AuthContext'
import { CartProvider } from './context/CartContext'

// ProtectedRoute redirects unauthenticated users to /login.
// adminOnly additionally requires the "admin" role.
function ProtectedRoute({ children, adminOnly = false }: { children: ReactNode; adminOnly?: boolean }) {
  const { user, isAdmin } = useAuth()
  if (!user) return <Navigate to="/login" replace />
  if (adminOnly && !isAdmin) return <Navigate to="/" replace />
  return <>{children}</>
}

// AppRoutes is a child of AuthProvider so ProtectedRoute can call useAuth().
function AppRoutes() {
  const { user } = useAuth()
  return (
    <>
      <Navbar />
      <Routes>
        <Route path="/" element={<ProductCatalog />} />
        <Route path="/cart" element={<Cart />} />
        {/* Redirect already-logged-in users away from the login page */}
        <Route path="/login" element={user ? <Navigate to="/" replace /> : <Login />} />
        <Route path="/orders" element={<ProtectedRoute><OrderList /></ProtectedRoute>} />
        <Route path="/orders/:id" element={<ProtectedRoute><OrderDetail /></ProtectedRoute>} />
        <Route path="/admin" element={<ProtectedRoute adminOnly><Admin /></ProtectedRoute>} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </>
  )
}

// Provider order matters:
//   AuthProvider   — auth state (user, token)
//   CartProvider   — cart state (items) — inside Auth so cart can read auth if needed
//   BrowserRouter  — routing context — inside providers so hooks can be used in route components
export default function App() {
  return (
    <AuthProvider>
      <CartProvider>
        <BrowserRouter>
          <AppRoutes />
        </BrowserRouter>
      </CartProvider>
    </AuthProvider>
  )
}
