import { createContext, useContext, type ReactNode } from 'react'
import { useCart } from '../hooks/useCart'

// CartProvider lifts cart state to a single shared instance so ProductCatalog
// and Cart can both read/write the same items without prop drilling.
type CartContextValue = ReturnType<typeof useCart>
const CartContext = createContext<CartContextValue | null>(null)

export function CartProvider({ children }: { children: ReactNode }) {
  const cart = useCart()
  return <CartContext.Provider value={cart}>{children}</CartContext.Provider>
}

export function useCartContext() {
  const ctx = useContext(CartContext)
  if (!ctx) throw new Error('useCartContext must be used within CartProvider')
  return ctx
}
