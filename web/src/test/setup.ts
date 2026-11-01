import '@testing-library/jest-dom/vitest'
import { cleanup, configure } from '@testing-library/react'
import { server } from '@/test/server'

// Pages wait on several queries; with a worker per file on a busy machine the
// default 1 s wait for an element is sometimes too short.
configure({ asyncUtilTimeout: 5000 })

// Listen before test files import modules: openapi-fetch captures fetch when the client is created.
server.listen({ onUnhandledFrame: 'error' })

afterEach(() => {
  cleanup()
  delete document.body.dataset.marker
  server.resetHandlers()
})
afterAll(() => {
  server.close()
})

// uPlot draws on canvas and MapLibre on WebGL, neither of which jsdom has: tests
// get stand-ins that record what they were given. Playwright covers the real ones.
// A chart's element counts how often it was built (data-builds) and keeps its
// cursor hook so tests can hover (hoverChart).
interface ChartElement extends HTMLElement {
  setCursor?: (idx: number | null) => void
}
vi.mock('uplot', () => ({
  default: class {
    constructor(
      opts: { hooks: { setCursor: ((u: { cursor: { idx: number | null } }) => void)[] } },
      data: unknown[][],
      el: ChartElement,
    ) {
      el.dataset.points = String(data[0]?.length ?? 0)
      el.dataset.series = String(data.length - 1)
      el.dataset.builds = String(Number(el.dataset.builds ?? 0) + 1)
      el.setCursor = (idx) => {
        opts.hooks.setCursor[0]?.({ cursor: { idx } })
      }
    }
    setSize() {}
    destroy() {}
  },
}))
vi.mock('maplibre-gl', () => {
  class LngLatBounds {
    extend() {
      return this
    }
  }
  class Map {
    constructor(opts: {
      container: HTMLElement
      style: { sources: { tiles: { tiles: string[] } } }
    }) {
      // A tile URL naming "no-webgl" stands in for a browser without WebGL2.
      if (opts.style.sources.tiles.tiles[0]?.includes('no-webgl'))
        throw new Error('WebGL2 is required')
      opts.container.dataset.map = 'ready'
    }
    remove() {}
  }
  // The marker shows where it is on document.body (data-marker).
  class Marker {
    at = ''
    setLngLat([lng, lat]: [number, number]) {
      this.at = `${String(lng)},${String(lat)}`
      return this
    }
    addTo() {
      document.body.dataset.marker = this.at
      return this
    }
    remove() {
      delete document.body.dataset.marker
    }
  }
  return { LngLatBounds, Map, Marker }
})

// jsdom has no layout, so nothing ever resizes.
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
}

// hoverChart moves a chart's cursor to sample idx, as hovering it would.
export function hoverChart(el: HTMLElement, idx: number | null) {
  ;(el as ChartElement).setCursor?.(idx)
}
