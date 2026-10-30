import { useEffect, useRef, useState } from 'react'
import * as maplibregl from 'maplibre-gl'
import 'maplibre-gl/dist/maplibre-gl.css'
import { cssColor } from '@/lib/utils'

export interface RouteMapProps {
  tileUrl: string
  lat: (number | null)[]
  lon: (number | null)[]
  // The sample hovered in a chart, shown as a marker.
  hoverIndex: number | null
}

// RouteMap draws the route on raster tiles from the configured tile URL.
export function RouteMap({ tileUrl, lat, lon, hoverIndex }: RouteMapProps) {
  const ref = useRef<HTMLDivElement>(null)
  const marker = useRef<maplibregl.Marker | null>(null)
  const map = useRef<maplibregl.Map | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    const el = ref.current
    const coords: [number, number][] = []
    lat.forEach((la, i) => {
      const lo = lon[i]
      if (la !== null && lo !== null && lo !== undefined) coords.push([lo, la])
    })
    if (!el || coords.length === 0) return
    const bounds = coords.reduce(
      (b, c) => b.extend(c),
      new maplibregl.LngLatBounds(coords[0], coords[0]),
    )
    let m: maplibregl.Map
    try {
      m = new maplibregl.Map({
        container: el,
        style: {
          version: 8,
          sources: {
            tiles: {
              type: 'raster',
              tiles: [tileUrl],
              tileSize: 256,
              attribution: tileUrl.includes('openstreetmap.org')
                ? '© OpenStreetMap contributors'
                : '',
            },
            route: {
              type: 'geojson',
              data: {
                type: 'Feature',
                properties: {},
                geometry: { type: 'LineString', coordinates: coords },
              },
            },
          },
          layers: [
            { id: 'tiles', type: 'raster', source: 'tiles' },
            {
              id: 'route',
              type: 'line',
              source: 'route',
              paint: { 'line-color': cssColor('--chart-2'), 'line-width': 3 },
            },
          ],
        },
        bounds,
        fitBoundsOptions: { padding: 24 },
      })
    } catch {
      // MapLibre needs WebGL2; without it the rest of the page still works.
      queueMicrotask(() => {
        setFailed(true)
      })
      return
    }
    map.current = m
    marker.current = new maplibregl.Marker()
    return () => {
      marker.current = null
      map.current = null
      m.remove()
    }
  }, [tileUrl, lat, lon])

  useEffect(() => {
    const m = map.current
    const mk = marker.current
    if (!m || !mk) return
    const la = hoverIndex === null ? null : lat[hoverIndex]
    const lo = hoverIndex === null ? null : lon[hoverIndex]
    if (la === null || la === undefined || lo === null || lo === undefined) {
      mk.remove()
      return
    }
    mk.setLngLat([lo, la]).addTo(m)
  }, [hoverIndex, lat, lon])

  if (failed) {
    return (
      <p role="note" className="text-sm text-muted-foreground">
        This browser cannot display maps: it needs WebGL2.
      </p>
    )
  }
  return <div ref={ref} role="region" aria-label="Route map" className="h-80 w-full rounded-md" />
}
