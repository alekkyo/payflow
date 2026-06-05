import { createContext, useContext, useState, useCallback, type ReactNode } from 'react'
import { auth, type User } from '../api/client'

type AuthContextValue = {
  user: User | null
  login: (email: string, password: string) => Promise<User>
  register: (email: string, password: string, name: string) => Promise<User>
  logout: () => Promise<void>
  isAdmin: boolean
}

const AuthContext = createContext<AuthContextValue | null>(null)

function getStoredUser(): User | null {
  try {
    const raw = localStorage.getItem('user')
    return raw ? JSON.parse(raw) : null
  } catch {
    return null
  }
}

// AuthProvider holds the single source of truth for auth state.
// All components that call useAuth() share this same state instance,
// so a login in one component is immediately visible in Navbar, etc.
export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(getStoredUser)

  const login = useCallback(async (email: string, password: string) => {
    const res = await auth.login(email, password)
    localStorage.setItem('token', res.token)
    localStorage.setItem('user', JSON.stringify(res.user))
    setUser(res.user)
    return res.user
  }, [])

  const register = useCallback(async (email: string, password: string, name: string) => {
    const res = await auth.register(email, password, name)
    localStorage.setItem('token', res.token)
    localStorage.setItem('user', JSON.stringify(res.user))
    setUser(res.user)
    return res.user
  }, [])

  const logout = useCallback(async () => {
    await auth.logout().catch(() => {})
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    setUser(null)
  }, [])

  return (
    <AuthContext.Provider value={{ user, login, register, logout, isAdmin: user?.role === 'admin' }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
