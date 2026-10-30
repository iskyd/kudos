export { cn } from 'cn'

// cssColor reads a design token for drawing on canvas, where CSS variables do not apply.
export function cssColor(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || 'currentColor'
}
