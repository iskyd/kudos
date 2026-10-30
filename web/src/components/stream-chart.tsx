import { useEffect, useRef } from 'react'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { formatDuration } from '@/lib/format/units'
import { cssColor } from '@/lib/utils'

// Charts with the same key share one hover cursor.
const syncKey = 'activity'
const height = 160

export interface StreamChartProps {
  label: string
  // Seconds from the activity start.
  time: number[]
  values: (number | null)[]
  // Formats an axis value; values are SI, formatting goes through lib/format.
  format: (value: number) => string
  // Draws lower values higher, as for pace.
  invert?: boolean
  onHover: (index: number | null) => void
}

// StreamChart draws one stream with uPlot (canvas); hovering reports the sample index.
export function StreamChart({ label, time, values, format, invert, onHover }: StreamChartProps) {
  const ref = useRef<HTMLDivElement>(null)
  const hover = useRef(onHover)
  useEffect(() => {
    hover.current = onHover
  })

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const axisColor = cssColor('--muted-foreground')
    const chart = new uPlot(
      {
        width: el.clientWidth || 600,
        height,
        cursor: { sync: { key: syncKey } },
        legend: { show: false },
        scales: { x: { time: false }, y: { dir: invert ? -1 : 1 } },
        axes: [
          { stroke: axisColor, values: (_, ticks) => ticks.map((t) => formatDuration(t)) },
          { stroke: axisColor, size: 80, values: (_, ticks) => ticks.map((t) => format(t)) },
        ],
        series: [{}, { label, stroke: cssColor('--chart-2'), width: 1.5 }],
        hooks: {
          setCursor: [
            (u) => {
              hover.current(u.cursor.idx ?? null)
            },
          ],
        },
      },
      [time, values],
      el,
    )
    const resize = new ResizeObserver(() => {
      chart.setSize({ width: el.clientWidth || 600, height })
    })
    resize.observe(el)
    return () => {
      resize.disconnect()
      chart.destroy()
    }
  }, [label, time, values, format, invert])

  return (
    <figure className="flex flex-col gap-1">
      <figcaption className="text-sm font-medium">{label}</figcaption>
      <div ref={ref} role="img" aria-label={`${label} chart`} />
    </figure>
  )
}
