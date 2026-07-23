/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        pf: {
          bg: '#f5ead8',
          surface: '#ebddc5',
          text: '#201e1d',
          accent: {
            DEFAULT: '#c67139',
            100: '#fff2eb',
            200: '#ffe1d0',
            300: '#ffc6a5',
            400: '#f6a06b',
            500: '#d67f48',
            600: '#b2622d',
            700: '#8c491a',
            800: '#643312',
            900: '#402310',
          },
          sage: {
            DEFAULT: '#7a8a5e',
            100: '#f0fae1',
            200: '#e1eecc',
            300: '#ccdbb2',
            400: '#aebf92',
            500: '#8fa073',
            600: '#728157',
            700: '#56633f',
            800: '#3d472b',
            900: '#272e1b',
          },
          neutral: {
            100: '#f9f4ed',
            200: '#eee7db',
            300: '#dcd3c4',
            400: '#c0b6a5',
            500: '#a19786',
            600: '#82796a',
            700: '#645c50',
            800: '#474238',
            900: '#2e2b25',
          },
        },
      },
      fontFamily: {
        heading: ['Caprasimo', 'system-ui', 'sans-serif'],
        body: ['Figtree', 'system-ui', 'sans-serif'],
      },
      borderRadius: {
        'pf-sm': '8px',
        'pf-md': '16px',
        'pf-lg': '28px',
        'pf-xl': '32px',
      },
      boxShadow: {
        'pf-sm': '0 1px 2px rgba(46,43,37,.14)',
        'pf-md': '0 3px 10px rgba(46,43,37,.16)',
        'pf-lg': '0 12px 32px rgba(46,43,37,.22)',
      },
      animation: {
        'pf-pop': 'pf-pop .35s ease',
        'pf-pulse': 'pf-pulse 1.4s infinite',
        'pf-spin-fast': 'pf-spin .7s linear infinite',
        'pf-shimmer': 'pf-shimmer 1.3s linear infinite',
        'pf-fade': 'pf-fade .28s ease',
        'pf-pulse-slow': 'pf-pulse 1.6s infinite',
      },
      keyframes: {
        'pf-pop': {
          '0%': { transform: 'scale(1)' },
          '40%': { transform: 'scale(1.18)' },
          '100%': { transform: 'scale(1)' },
        },
        'pf-pulse': {
          '0%': { boxShadow: '0 0 0 0 rgba(198,113,57,.45)' },
          '70%': { boxShadow: '0 0 0 9px transparent' },
          '100%': { boxShadow: '0 0 0 0 transparent' },
        },
        'pf-spin': {
          to: { transform: 'rotate(360deg)' },
        },
        'pf-shimmer': {
          '0%': { backgroundPosition: '200% 0' },
          '100%': { backgroundPosition: '-200% 0' },
        },
        'pf-fade': {
          from: { opacity: '0', transform: 'translateY(6px)' },
          to: { opacity: '1', transform: 'none' },
        },
      },
    },
  },
  plugins: [],
}
