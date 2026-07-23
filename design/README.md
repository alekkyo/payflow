# PayFlow — Storefront UI Redesign (Handoff)

## Overview
This folder is a **design reference** for a UI refresh of the PayFlow storefront: the customer-facing shop plus the operator admin dashboard. It covers five screens — **Login, Shop (catalog), Cart, Orders (live saga tracking), and Admin (payments ops dashboard)** — built in the warm, rounded "Organic" design system.

The goal of the redesign is to make the frontend look as polished as the Go backend deserves, so it reads well to a visiting employer.

## About the design files
The files here are a **design reference built in HTML/CSS/JS** — a prototype showing the intended look and behavior. **They are not meant to be dropped into the app as-is.** The task is to **recreate these designs inside the existing `frontend/` app** (React 19 + TanStack Query + React Router v7 + Tailwind CSS), wired to the real API, using the codebase's established patterns.

- `PayFlow.dc.html` — the full interactive prototype. It's a self-contained component file; open it in a browser to click through every screen and interaction.
- `organic.css` — the design-system stylesheet. **This is the source of truth for every color, font, radius, spacing and shadow.** Port these tokens into your Tailwind theme (see Design Tokens below) rather than eyeballing values.

> How to read `PayFlow.dc.html`: it's a custom component format. The **markup** (between `<x-dc>…</x-dc>`) is the layout; the **`class Component`** script at the bottom holds all state and behavior (product data, cart, saga animation, admin data). Read both to understand a screen. Ignore `support.js` — it's just the prototype runtime.

## Fidelity
**High-fidelity.** Colors, typography, spacing, radii, shadows, copy, and interactions are all final. Recreate the UI faithfully in React + Tailwind, using the exact token values below. Swap the prototype's hardcoded product/order/admin data for real API calls.

## How to implement (for Claude Code)

1. **Add the design tokens to Tailwind.** Copy the values from the Design Tokens section (or read them from `organic.css` `:root`) into `frontend/tailwind.config.js` (`theme.extend.colors`, `borderRadius`, `boxShadow`, `fontFamily`). Load the two Google fonts (Caprasimo, Figtree) in `index.html`.
2. **Set the base type + link colors.** Body uses Figtree; all headings use Caprasimo. Default and hover link colors come from `--color-accent`.
3. **Build one screen at a time**, in this order: Login → Shop → Cart → Orders → Admin. For each, read the matching section below and the corresponding block in `PayFlow.dc.html`.
4. **Wire to the real API** (`/products`, `/orders`, `/payments`, `/admin`, and the SSE stream `GET /orders/:id/events/stream`). The prototype fakes these; see State Management for what each screen needs.
5. **Keep the interactions**: add-to-cart flash, qty steppers, category/search filtering, the live saga timeline (this is the hero — drive it from real SSE events), loading skeletons, and empty/error states.

## Screens / Views

### 1. Login
- **Purpose**: Authenticate before entering the store.
- **Layout**: Centered card, `max-width: 880px`, two columns via `grid-template-columns: repeat(auto-fit, minmax(300px, 1fr))` so it stacks on narrow screens. Radius `--radius-lg` (28px), `--shadow-lg`, `overflow: hidden`.
  - **Left panel**: background `--color-accent-2-600` (#728157), text `--color-bg`. Contains the logo lockup, headline "Payments that just work.", a one-line description, and three check-marked feature rows (Distributed saga orchestration / Idempotent payments & webhooks / Real-time order tracking via SSE). Padding `42px 38px`.
  - **Right panel**: the form. Kicker "Sign in · test mode", h3 "Welcome back", Email + Password fields (`.field` label + `.input`), a full-width primary "Sign in" button, then two secondary "demo account" quick-fill buttons (Admin / Customer) showing the demo emails.
- **Behavior**: Sign-in button is **disabled until both fields are non-empty**. Demo buttons fill the fields with the README credentials (`admin@payflow.dev` / `demo-admin-123`, `customer@payflow.dev` / `demo-customer-123`). On submit, authenticate against the API and route to Shop. The top nav is **hidden** on this screen.

### 2. Shop (catalog)
- **Purpose**: Browse products and add to cart.
- **Layout**: Sticky top nav, then a `max-width: 1180px` centered container. Header row (kicker + large h1 "Good electronics, honestly priced." + subtitle) with a search input on the right, wrapping on narrow. A row of category filter pills. Product grid: `grid-template-columns: repeat(auto-fill, minmax(230px, 1fr))`, gap 20px.
- **Product card**: surface background, radius `--radius-lg`, `--shadow-sm`; on hover lift `translateY(-5px)` + `--shadow-lg` (transition .18s). Top image area `aspect-ratio: 4/3` on `--color-accent-100` with a faint 45° stripe pattern (placeholder for a real product photo) — a category tag top-left, a stock tag top-right when relevant. Body: title (Caprasimo 17px), muted blurb, then a price (Caprasimo 22px) and an Add button.
- **States**: category filter + text search both filter the grid; **out-of-stock** products show a "Sold out" tag and a disabled button; **low stock** (≤10) shows "Only N left"; empty result shows a "Nothing matches that" empty state with a Clear-filters button.
- **Add-to-cart micro-interaction**: button briefly swaps "＋ Add" → "✓ Added" with a `pf-pop` scale animation (~350ms) and the nav cart badge increments.

### 3. Cart
- **Purpose**: Review items and place the order.
- **Layout**: h1 "Your cart", then a two-column grid `minmax(0,1.7fr) minmax(280px,1fr)` (items left, summary right; stacks on narrow).
  - **Item row**: 74px thumbnail, name + "category · $X each", a pill qty stepper (− qty +), line total (Caprasimo 18px), and a ghost trash/remove button.
  - **Summary card**: sticky (`top: 88px`), surface bg, `--shadow-md`. Subtotal, Shipping (free ≥ $200, else $9, with a "add $X more" hint), a rule, Total (Caprasimo 26px), a full-width "Place order" button, and a "Secured by Stripe · idempotency-key sent" line.
- **States**: empty cart shows a centered empty state (bag icon, "Your cart is empty", "Browse products" button).
- **Behavior**: **Checkout is direct** — no payment form. "Place order" creates the order via the API (send an `Idempotency-Key` header), clears the cart, and routes to Orders where the saga streams live.

### 4. Orders (hero: live saga timeline)
- **Purpose**: Track orders in real time; this screen is the centerpiece.
- **Layout**: h1 "Your orders", then a large **hero card** for the most recent order, then an "Order history" list.
  - **Hero header**: "Order {id}" + a status tag (In progress / Fulfilled / Payment failed), items + placed-at on the left; total on the right with either a live "● Live · streaming via SSE" indicator (pulsing dot) or, when done, a "Replay stream" button.
  - **Timeline**: a vertical 6-stage list (Order placed → Inventory reserved → Payment processing → Payment captured → Order confirmed → Fulfilled). Each row: a 26px status dot + a connector line, then label (Caprasimo 16px) + description + a monospace timestamp. Dot states: **done** = filled `--color-accent-2-500` with a check; **active** = `--color-accent-100` ring on `--color-accent`, pulsing, with a small spinner; **failed** = `--color-accent-700` with an alert glyph; **pending** = hollow `--color-neutral-400` ring. Connector is `--color-accent-2-500` up to the reached stage, else `--color-neutral-300`.
  - **Connecting state**: before the stream starts, show a spinner + "Connecting to live stream…" and three shimmer skeleton lines.
- **Order history**: compact cards with id, status tag, items, placed-at, total. A **failed** order shows an inline `--color-accent-100` banner with the decline reason and a "Retry payment" button (which restarts the saga live).
- **Behavior**: drive the timeline from real **Server-Sent Events** on `GET /orders/:id/events/stream` — advance the reached stage as each `status` event arrives. The prototype simulates this by advancing one stage every 1500ms after a 900ms "connect" delay.

### 5. Admin (payments ops dashboard)
- **Purpose**: Operator view of the payment system's health.
- **Layout**: header (kicker "Operations · live" + h1 "Payments dashboard" + an "All systems nominal" pill), then stacked sections, all `max-width: 1180px`. On first visit show **loading skeletons** (~750ms) before the data.
  - **Metric cards**: `repeat(auto-fit, minmax(200px, 1fr))` — Revenue today $12,480, Orders today 143, Payment success 98.6%, Avg fulfilment 4.2s. Each: muted label, Caprasimo 32px value, colored delta (▲ green `--color-accent-2-700` / ▼ terracotta `--color-accent-700`).
  - **Charts** (`repeat(auto-fit, minmax(340px, 1fr))`): a **Revenue** SVG area+line chart (last 14 days, accent stroke + gradient fill) and an **Order volume** CSS bar chart (14 sage bars).
  - **Redis stream depths**: five rows (inventory.reserve, payment.process, webhook.handle, refund.issue, reconcile.daily), each a labeled progress bar with depth + rate; a backed-up queue uses `--color-accent-500` instead of `--color-accent-2-500`.
  - **Dead-letter queue**: list of stuck messages (id · stream, error, age) with a Retry/Inspect button; a header tag shows the count.
  - **Daily reconciliation vs Stripe**: run time / checked / matched summary, and a `.table` of discrepancies (amount mismatch, missing local).
  - **Order saga monitor**: a `.table` of recent orders with a monospace stage tag, amount, and age.
- **Behavior**: replace all hardcoded numbers with `/admin` (or `/metrics`) API data. Keep the skeleton-on-load pattern.

## Interactions & behavior
- **Navigation**: sticky top nav with Shop / Orders / Admin links (active link uses `aria-current="page"`, colored `--color-accent`), a GitHub icon link (opens `https://github.com/alekkyo/payflow` in a new tab), a Cart button with a live count badge, and a Sign-out action. Nav is hidden on Login.
- **Animations** (see `@keyframes` in the file): `pf-pop` (add-to-cart, .35s), `pf-pulse` (live/active dots, ~1.4s loop), `pf-spin` (spinners, .7s), `pf-shimmer` (skeletons, 1.3s), `pf-fade` (view enter, .28s). Card hover lift is a .18s transform+shadow transition.
- **Focus**: keyboard focus is a `2px solid var(--color-accent)` ring at `offset: 2px` (already in `organic.css`) — keep it, don't use browser defaults.
- **Responsive**: achieved with intrinsic layout (`auto-fit`/`auto-fill` grids, `flex-wrap`, `max-width` containers) — no fixed breakpoints required, but verify at ~375px, 768px, and 1180px.

## State management
- **Auth**: current user/session; gate the app on it; expose sign-out.
- **Cart**: `{ [productId]: qty }`; derive count, subtotal, shipping, total. Persist if desired.
- **Products**: from `GET /products` (fields used: name, category, price in cents, stock, blurb/description). Client-side category + search filtering.
- **Orders**: list from `GET /orders`; each has items, total (cents), status, and a stage/event history. The live order subscribes to the **SSE** stream and advances its stage on each event; failed orders carry a decline reason and support retry (`POST /payments` again with a fresh idempotency key).
- **Admin**: metrics, queue depths, dead-letter messages, reconciliation discrepancies, and recent saga rows from the admin endpoints. Loading flag for the initial skeleton.
- **Money**: all amounts are integer **cents** end-to-end; format to whole dollars for display (e.g. `$178`). Never use floats.

## Design tokens
All values live in `organic.css` `:root`. Key ones:

**Colors**
- bg `#f5ead8` · surface `#ebddc5` · text `#201e1d`
- accent (terracotta) `#c67139` · accent-2 (sage) `#7a8a5e`
- divider = text @ 16% alpha
- Accent ramp 100→900: `#fff2eb #ffe1d0 #ffc6a5 #f6a06b #d67f48 #b2622d #8c491a #643312 #402310`
- Accent-2 ramp 100→900: `#f0fae1 #e1eecc #ccdbb2 #aebf92 #8fa073 #728157 #56633f #3d472b #272e1b`
- Neutral ramp 100→900: `#f9f4ed #eee7db #dcd3c4 #c0b6a5 #a19786 #82796a #645c50 #474238 #2e2b25`

**Typography**
- Headings: **Caprasimo** 400. Body: **Figtree** 400/600/700. Base body 15px / line-height 1.55.
- Scale: h1 42px, h2 32px, h3 25px, h4 20px; heading line-height 1.12, letter-spacing -0.015em. (The prototype bumps h1 to 44–46px on hero screens.)

**Radius**: sm 8px · md 16px · lg 28px. Buttons/inputs/tags/segmented go fully pill (`border-radius: 999px`); cards/dialogs use ~`lg × 1.15`.

**Shadows** (ink-tinted, tuned to the ground):
- sm `0 1px 2px rgba(46,43,37,.14)` · md `0 3px 10px rgba(46,43,37,.16)` · lg `0 12px 32px rgba(46,43,37,.22)`

**Spacing scale** (1.10× density): 4.4 / 8.8 / 13.2 / 17.6 / 26.4 / 35.2 px.

## Component classes (from organic.css)
`.btn` (+ `.btn-primary/-secondary/-ghost/-icon/-block`), `.tag` (+ `.tag-accent/-accent-2/-neutral/-outline`), `.card` (+ kicker/title/body/meta, `.elev-sm/md/lg`), `.field`/`.input`/`.radio`/`.seg`, `.nav`/`.nav-brand`, `.table`, `.dialog`. Reuse these patterns as your Tailwind component classes.

## Assets
- **Logo**: a coin mark — a circular `--color-accent` (or `--color-bg` on the sage panel) chip with an inset ring and a Caprasimo "$". No image file; recreate with CSS.
- **Icons**: Lucide (https://lucide.dev), stroke-width ~2.75, drawn inline as SVG. The GitHub mark is the standard Octocat glyph on the header link.
- **Product images**: the prototype uses striped placeholders. Wrap real product photos in the design-system `.washed` treatment (desaturated, lifted) as described in the DS.
- **Fonts**: Caprasimo + Figtree via Google Fonts.

## Files in this bundle
- `PayFlow.dc.html` — the full interactive prototype (markup + logic).
- `organic.css` — the design-system tokens + component classes (source of truth).
- `README.md` — this document.
