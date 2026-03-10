'use client'

import { useRef, useEffect, useState, useCallback } from 'react'
import { cn } from '@/lib/utils'

interface ScaledSlideFrameProps {
  /** The HTML content to render in the slide */
  htmlContent: string
  /** Base width of the slide (default: 1920) */
  baseWidth?: number
  /** Base height of the slide (default: 1080) */
  baseHeight?: number
  /** Additional class names for the outer container */
  className?: string
  /** Whether to show a shadow around the slide */
  showShadow?: boolean
  /** Whether to show a border/ring around the slide */
  showBorder?: boolean
  /** Vertical alignment of the slide (default: 'center') */
  align?: 'top' | 'center'
}

/**
 * A reusable component that renders HTML content at a fixed resolution
 * and scales it proportionally to fit its container, like an image.
 * 
 * The HTML is rendered in a sandboxed iframe at the base dimensions,
 * then CSS transform is used to scale the entire frame to fit.
 */
export function ScaledSlideFrame({
  htmlContent,
  baseWidth = 1920,
  baseHeight = 1080,
  className,
  showShadow = false,
  showBorder = false,
  align = 'center',
}: ScaledSlideFrameProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [scale, setScale] = useState(0)

  const updateScale = useCallback(() => {
    const container = containerRef.current
    if (!container) return

    const containerWidth = container.clientWidth
    const containerHeight = container.clientHeight

    if (containerWidth === 0 || containerHeight === 0) return

    // Calculate scale to fit while maintaining aspect ratio
    const scaleX = containerWidth / baseWidth
    const scaleY = containerHeight / baseHeight
    const newScale = Math.min(scaleX, scaleY)

    setScale(newScale)
  }, [baseWidth, baseHeight])

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    // Initial calculation
    updateScale()

    // Watch for container size changes
    const resizeObserver = new ResizeObserver(updateScale)
    resizeObserver.observe(container)

    return () => resizeObserver.disconnect()
  }, [updateScale])

  // Calculate the scaled dimensions
  const scaledWidth = baseWidth * scale
  const scaledHeight = baseHeight * scale

  return (
    <div
      ref={containerRef}
      className={cn('relative w-full h-full overflow-hidden', className)}
    >
      {/* Alignment container */}
      <div className={cn(
        'absolute inset-0 flex justify-center',
        align === 'top' ? 'items-start' : 'items-center'
      )}>
        {/* The scaled slide wrapper */}
        <div
          className={cn(
            'relative overflow-hidden bg-slide-bg',
            showShadow && 'shadow-2xl',
            showBorder && 'border border-border rounded-lg'
          )}
          style={{
            width: scaledWidth,
            height: scaledHeight,
          }}
        >
          {/* The iframe rendered at native resolution, then scaled */}
          <iframe
            srcDoc={`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=${baseWidth}, initial-scale=1">
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
html, body { 
  width: ${baseWidth}px; 
  height: ${baseHeight}px; 
  overflow: hidden;
  background: white;
}
</style>
</head>
<body>${htmlContent}</body>
</html>`}
            className="border-0 pointer-events-none"
            style={{
              width: baseWidth,
              height: baseHeight,
              transform: `scale(${scale})`,
              transformOrigin: 'top left',
            }}
            sandbox="allow-same-origin"
            title="Slide content"
          />
        </div>
      </div>
    </div>
  )
}
