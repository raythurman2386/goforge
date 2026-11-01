/** @type {import('tailwindcss').Config} */

// Token colors are CSS variables holding space-separated RGB channels (see
// static/css/input.css). `rgb(var(--x) / <alpha-value>)` keeps opacity
// modifiers such as bg-primary-500/20 working on Tailwind 3.4.
const token = (name) => `rgb(var(--gf-${name}) / <alpha-value>)`;

module.exports = {
  // Dark mode follows the OS (prefers-color-scheme) only. No toggle, no stored preference.
  darkMode: 'media',
  content: [
    "./internal/web/templates/**/*.templ",
    "./internal/web/templates/**/*.go",
    "./internal/web/handlers/**/*.go",
    "./static/js/app.js",
    "./static/js/ui.js",
  ],
  theme: {
    extend: {
      colors: {
        // Emerald brand palette, unchanged values, now read from tokens.
        primary: {
          50: token('primary-50'),
          100: token('primary-100'),
          200: token('primary-200'),
          300: token('primary-300'),
          400: token('primary-400'),
          500: token('primary-500'),
          600: token('primary-600'),
          700: token('primary-700'),
          800: token('primary-800'),
          900: token('primary-900'),
          950: token('primary-950'),
        },
        // Semantic tokens. Light and dark values live in input.css under
        // prefers-color-scheme, so these need no dark: variant.
        background: token('background'),
        foreground: token('foreground'),
        card: {
          DEFAULT: token('card'),
          border: token('card-border'),
        },
        border: token('border'),
        divider: token('divider'),
        muted: {
          DEFAULT: token('muted'),
          foreground: token('muted-foreground'),
        },
        label: token('label'),
        field: {
          DEFAULT: token('field'),
          border: token('field-border'),
          placeholder: token('field-placeholder'),
        },
        secondary: {
          DEFAULT: token('secondary'),
          foreground: token('secondary-foreground'),
          hover: token('secondary-hover'),
          border: token('secondary-border'),
        },
        ring: {
          DEFAULT: token('ring'),
          offset: token('ring-offset'),
        },
      },
      borderRadius: {
        ui: 'var(--gf-radius)',
        'ui-lg': 'var(--gf-radius-lg)',
      },
      fontFamily: {
        // Geist Sans for UI and body text, Geist Mono for code, logs, and IDs.
        // Both are self-hosted in static/fonts (see static/css/input.css).
        sans: ['Geist', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['"Geist Mono"', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'Consolas', 'monospace'],
      },
      animation: {
        'fade-in': 'fadeIn 0.5s ease-out forwards',
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0', transform: 'translateY(5px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        }
      }
    },
  },
  plugins: [],
}
