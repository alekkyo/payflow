import { useState, type JSX } from 'react'
import { useQuery } from '@tanstack/react-query'
import { products } from '../api/client'
import { useCartContext } from '../context/CartContext'

const LOW_STOCK = 10

function fmt(cents: number) {
  return `$${Math.round(cents / 100)}`
}

const CAT_ICONS: Record<string, JSX.Element> = {
  Audio: (
    <svg viewBox="0 0 48 48" width="52" height="52" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <path d="M8 28v-4a16 16 0 0 1 32 0v4" />
      <rect x="6" y="26" width="6" height="10" rx="3" />
      <rect x="36" y="26" width="6" height="10" rx="3" />
    </svg>
  ),
  Input: (
    <svg viewBox="0 0 48 48" width="52" height="52" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <rect x="4" y="14" width="40" height="20" rx="4" />
      <line x1="12" y1="22" x2="12" y2="22" strokeWidth="3" strokeLinecap="round" />
      <line x1="20" y1="22" x2="20" y2="22" strokeWidth="3" strokeLinecap="round" />
      <line x1="28" y1="22" x2="28" y2="22" strokeWidth="3" strokeLinecap="round" />
      <line x1="36" y1="22" x2="36" y2="22" strokeWidth="3" strokeLinecap="round" />
      <line x1="16" y1="29" x2="32" y2="29" strokeWidth="3" strokeLinecap="round" />
    </svg>
  ),
  Video: (
    <svg viewBox="0 0 48 48" width="52" height="52" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <rect x="4" y="12" width="28" height="22" rx="4" />
      <path d="M32 18l12-6v24l-12-6V18z" />
      <circle cx="18" cy="23" r="5" />
    </svg>
  ),
  Cables: (
    <svg viewBox="0 0 48 48" width="52" height="52" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <rect x="6" y="10" width="14" height="20" rx="3" />
      <line x1="10" y1="10" x2="10" y2="6" />
      <line x1="16" y1="10" x2="16" y2="6" />
      <path d="M13 30v4a14 14 0 0 0 14 14" strokeDasharray="3 2" />
      <rect x="30" y="28" width="12" height="10" rx="3" />
      <line x1="36" y1="28" x2="36" y2="22" />
      <circle cx="36" cy="20" r="3" />
    </svg>
  ),
  Desk: (
    <svg viewBox="0 0 48 48" width="52" height="52" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <rect x="6" y="8" width="36" height="24" rx="3" />
      <line x1="24" y1="32" x2="24" y2="40" />
      <line x1="14" y1="40" x2="34" y2="40" />
      <circle cx="24" cy="20" r="1.5" fill="currentColor" />
    </svg>
  ),
  Electronics: (
    <svg viewBox="0 0 48 48" width="52" height="52" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <rect x="12" y="12" width="24" height="24" rx="3" />
      <line x1="18" y1="12" x2="18" y2="8" />
      <line x1="24" y1="12" x2="24" y2="8" />
      <line x1="30" y1="12" x2="30" y2="8" />
      <line x1="18" y1="36" x2="18" y2="40" />
      <line x1="24" y1="36" x2="24" y2="40" />
      <line x1="30" y1="36" x2="30" y2="40" />
      <line x1="12" y1="18" x2="8" y2="18" />
      <line x1="12" y1="24" x2="8" y2="24" />
      <line x1="12" y1="30" x2="8" y2="30" />
      <line x1="36" y1="18" x2="40" y2="18" />
      <line x1="36" y1="24" x2="40" y2="24" />
      <line x1="36" y1="30" x2="40" y2="30" />
      <rect x="19" y="19" width="10" height="10" rx="1" />
    </svg>
  ),
}

function categoryOf(name: string): string {
  const n = name.toLowerCase()
  if (n.includes('headphone') || n.includes('earphone') || n.includes('wireless')) return 'Audio'
  if (n.includes('keyboard')) return 'Input'
  if (n.includes('webcam') || n.includes('camera')) return 'Video'
  if (n.includes('hub') || n.includes('usb') || n.includes('cable') || n.includes('management')) return 'Cables'
  if (n.includes('monitor') || n.includes('arm') || n.includes('pad')) return 'Desk'
  return 'Electronics'
}

const CAT_COLORS: Record<string, { dot: string; bg: string; text: string }> = {
  Audio:       { dot: '#c67139', bg: '#fff2eb', text: '#643312' },
  Input:       { dot: '#7a8a5e', bg: '#f0fae1', text: '#3d472b' },
  Video:       { dot: '#8c491a', bg: '#ffe1d0', text: '#402310' },
  Cables:      { dot: '#a19786', bg: '#f9f4ed', text: '#474238' },
  Desk:        { dot: '#56633f', bg: '#e1eecc', text: '#272e1b' },
  Electronics: { dot: '#c67139', bg: '#fff2eb', text: '#643312' },
}

function Tag({ cat }: { cat: string }) {
  const c = CAT_COLORS[cat] ?? CAT_COLORS.Electronics
  return (
    <span
      className="inline-flex items-center text-[11px] px-2.5 py-0.5 rounded-full"
      style={{ background: c.bg, color: c.text }}
    >
      {cat}
    </span>
  )
}

export function ProductCatalog() {
  const [query, setQuery] = useState('')
  const [activeCat, setActiveCat] = useState('All')
  const [flashIds, setFlashIds] = useState<Set<string>>(new Set())

  const { data, isLoading, error } = useQuery({
    queryKey: ['products'],
    queryFn: () => products.list(),
  })

  const { data: invData } = useQuery({
    queryKey: ['products', 'inventory'],
    queryFn: () => products.inventory(),
    refetchInterval: 30_000,
    staleTime: 0,
  })

  const { addItem, items } = useCartContext()

  const productList = (data?.products ?? []).filter((p) => p.active)

  const cats = ['All', ...Array.from(new Set(productList.map((p) => categoryOf(p.name))))]

  const filtered = productList.filter((p) => {
    const matchCat = activeCat === 'All' || categoryOf(p.name) === activeCat
    const matchQ = query.trim() === '' || p.name.toLowerCase().includes(query.toLowerCase())
    return matchCat && matchQ
  })

  const handleAdd = (p: typeof productList[number]) => {
    addItem(p)
    setFlashIds((s) => new Set(s).add(p.id))
    setTimeout(() => {
      setFlashIds((s) => {
        const next = new Set(s)
        next.delete(p.id)
        return next
      })
    }, 350)
  }

  if (isLoading) {
    return (
      <div className="max-w-[1180px] mx-auto px-6 pb-20">
        <div className="pt-10 pb-6">
          <div className="pf-shim h-5 w-36 mb-3" />
          <div className="pf-shim h-12 w-96 mb-2" />
          <div className="pf-shim h-4 w-72" />
        </div>
        <div
          className="gap-5"
          style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(230px, 1fr))' }}
        >
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="pf-shim h-[300px]" />
          ))}
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="max-w-[1180px] mx-auto px-6 py-16 text-center text-pf-accent-700">
        Failed to load products.
      </div>
    )
  }

  return (
    <div className="max-w-[1180px] mx-auto px-6 pb-20 animate-pf-fade">
      {/* Header */}
      <div className="flex justify-between items-end gap-6 flex-wrap pt-10 pb-7">
        <div style={{ maxWidth: 560 }}>
          <div className="text-[12px] uppercase tracking-[0.1em] mb-1" style={{ color: '#c67139' }}>
            Demo storefront · test mode
          </div>
          <h1 className="font-heading text-[46px] mb-2">
            Good electronics,<br />honestly priced.
          </h1>
          <p className="text-[15px] m-0" style={{ color: 'rgba(32,30,29,.55)' }}>
            Every order runs the real saga — inventory, payment, fulfilment — live.
          </p>
        </div>

        {/* Search */}
        <label
          className="flex items-center gap-2 px-3.5 py-2 rounded-full text-[14px]"
          style={{
            width: 'min(300px, 100%)',
            background: '#ebddc5',
            border: '1px solid rgba(32,30,29,.16)',
          }}
        >
          <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" strokeWidth="2.75" strokeLinecap="round" strokeLinejoin="round" style={{ opacity: 0.6, flexShrink: 0 }}>
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.3-4.3" />
          </svg>
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search products…"
            className="border-0 bg-transparent outline-none text-[14px] w-full"
            style={{ color: '#201e1d', fontFamily: 'inherit' }}
          />
        </label>
      </div>

      {/* Category pills */}
      <div className="flex gap-2 mb-7 flex-wrap">
        {cats.map((cat) => {
          const active = cat === activeCat
          return (
            <button
              key={cat}
              onClick={() => setActiveCat(cat)}
              className="px-4 py-[7px] rounded-full text-[13px] transition-colors font-body border-0"
              style={{
                background: active ? '#c67139' : '#ebddc5',
                color: active ? '#f5ead8' : '#201e1d',
                cursor: 'pointer',
              }}
            >
              {cat}
            </button>
          )
        })}
      </div>

      {/* Grid */}
      {filtered.length === 0 ? (
        <div className="text-center py-24" style={{ color: 'rgba(32,30,29,.55)' }}>
          <div className="font-heading text-[22px] text-pf-text mb-1.5">Nothing matches that</div>
          <p className="m-0 mb-4">Try another search or category.</p>
          <button
            onClick={() => { setQuery(''); setActiveCat('All') }}
            className="px-4 py-[7px] rounded-full text-[14px] font-heading transition-colors border"
            style={{ borderColor: 'rgba(32,30,29,.16)', background: 'transparent', cursor: 'pointer' }}
          >
            Clear filters
          </button>
        </div>
      ) : (
        <div
          className="gap-5"
          style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(230px, 1fr))' }}
        >
          {filtered.map((product) => {
            const available = invData?.inventory?.[product.id]
            const isOut = available !== undefined && available <= 0
            const isLow = available !== undefined && available > 0 && available <= LOW_STOCK
            const inCart = items.find((i) => i.product.id === product.id)
            const flashed = flashIds.has(product.id)
            const cat = categoryOf(product.name)

            return (
              <div
                key={product.id}
                className="flex flex-col overflow-hidden transition-[transform,box-shadow] duration-[180ms] ease-out hover:-translate-y-1"
                style={{
                  background: '#ebddc5',
                  borderRadius: 28,
                  boxShadow: '0 1px 2px rgba(46,43,37,.14)',
                }}
                onMouseEnter={(e) => {
                  ;(e.currentTarget as HTMLElement).style.boxShadow = '0 12px 32px rgba(46,43,37,.22)'
                }}
                onMouseLeave={(e) => {
                  ;(e.currentTarget as HTMLElement).style.boxShadow = '0 1px 2px rgba(46,43,37,.14)'
                }}
              >
                {/* Image area */}
                <div
                  className="relative flex items-center justify-center"
                  style={{
                    aspectRatio: '4/3',
                    background: '#fff2eb',
                    backgroundImage:
                      'repeating-linear-gradient(-45deg, rgba(198,113,57,.08) 0 1px, transparent 1px 13px)',
                  }}
                >
                  {/* Category tag — absolute top-left, per prototype */}
                  <span style={{ position: 'absolute', top: 12, left: 12 }}>
                    <Tag cat={cat} />
                  </span>
                  {isLow && (
                    <span
                      className="absolute top-3 right-3 text-[11px] px-2.5 py-0.5 rounded-full"
                      style={{ background: '#f5ead8', border: '1px solid #c67139', color: '#c67139' }}
                    >
                      Only {available} left
                    </span>
                  )}
                  {isOut && (
                    <span
                      className="absolute top-3 right-3 text-[11px] px-2.5 py-0.5 rounded-full"
                      style={{ background: '#f5ead8', border: '1px solid #c67139', color: '#c67139' }}
                    >
                      Sold out
                    </span>
                  )}
                  {product.image_url ? (
                    <img
                      src={product.image_url}
                      alt={product.name}
                      style={{
                        width: '100%',
                        height: '100%',
                        objectFit: 'cover',
                        position: 'absolute',
                        inset: 0,
                        borderRadius: '28px 28px 0 0',
                      }}
                    />
                  ) : (
                    <span style={{ color: 'rgba(32,30,29,.22)' }}>
                      {CAT_ICONS[cat] ?? CAT_ICONS.Electronics}
                    </span>
                  )}
                </div>

                {/* Body */}
                <div className="flex flex-col gap-1.5 flex-1 p-4 pb-[18px]">
                  <div className="font-heading text-[17px] leading-[1.2]">{product.name}</div>
                  <p
                    className="text-[12.5px] m-0 flex-1"
                    style={{ color: 'rgba(32,30,29,.55)' }}
                  >
                    {product.description}
                  </p>
                  <div className="flex items-center justify-between gap-2 mt-1.5">
                    <span className="font-heading text-[22px]">{fmt(product.price_cents)}</span>
                    {isOut ? (
                      <button
                        disabled
                        className="px-4 py-[9px] rounded-full text-[14px] font-heading border opacity-45"
                        style={{ borderColor: 'rgba(32,30,29,.16)', background: 'transparent', cursor: 'not-allowed' }}
                      >
                        Sold out
                      </button>
                    ) : (
                      <button
                        onClick={() => handleAdd(product)}
                        className="px-4 py-[9px] rounded-full text-[14px] font-heading transition-colors"
                        style={{
                          background: '#c67139',
                          color: '#f5ead8',
                          border: 'none',
                          cursor: 'pointer',
                        }}
                      >
                        <span
                          className="inline-flex items-center gap-1.5"
                          style={{ animation: flashed ? 'pf-pop .35s ease' : undefined }}
                        >
                          {flashed ? (
                            <>
                              <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="2.75" strokeLinecap="round" strokeLinejoin="round">
                                <path d="M20 6 9 17l-5-5" />
                              </svg>
                              Added{inCart ? ` (${inCart.quantity})` : ''}
                            </>
                          ) : (
                            <>
                              <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="2.75" strokeLinecap="round" strokeLinejoin="round">
                                <path d="M5 12h14" />
                                <path d="M12 5v14" />
                              </svg>
                              {inCart ? `In cart (${inCart.quantity})` : 'Add'}
                            </>
                          )}
                        </span>
                      </button>
                    )}
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
