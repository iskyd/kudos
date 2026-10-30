import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { server } from '@/test/server'

// Listen before test files import modules: openapi-fetch captures fetch when the client is created.
server.listen({ onUnhandledFrame: 'error' })

afterEach(() => {
  cleanup()
  server.resetHandlers()
})
afterAll(() => {
  server.close()
})

// uPlot draws on canvas and MapLibre on WebGL, neither of which jsdom has: tests
// get stand-ins that record what they were given. Playwright covers the real ones.
vi.mock('uplot', () => ({
  default: class {
    constructor(_opts: unknown, data: unknown[][], el: HTMLElement) {
      el.dataset.points = String(data[0]?.length ?? 0)
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
  class Marker {
    setLngLat() {
      return this
    }
    addTo() {
      return this
    }
    remove() {}
  }
  return { LngLatBounds, Map, Marker }
})

// jsdom has no layout, so nothing ever resizes.
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
}
