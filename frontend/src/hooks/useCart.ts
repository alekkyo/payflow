import { useState, useCallback } from 'react'
import type { Product } from '../api/client'

export type CartItem = {
  product: Product
  quantity: number
}

export function useCart() {
  const [items, setItems] = useState<CartItem[]>([])

  const addItem = useCallback((product: Product, quantity = 1) => {
    setItems((prev) => {
      const existing = prev.find((i) => i.product.id === product.id)
      if (existing) {
        return prev.map((i) =>
          i.product.id === product.id ? { ...i, quantity: i.quantity + quantity } : i,
        )
      }
      return [...prev, { product, quantity }]
    })
  }, [])

  const removeItem = useCallback((productId: string) => {
    setItems((prev) => prev.filter((i) => i.product.id !== productId))
  }, [])

  const clear = useCallback(() => setItems([]), [])

  const totalCents = items.reduce((sum, i) => sum + i.product.price_cents * i.quantity, 0)

  return { items, addItem, removeItem, clear, totalCents }
}
