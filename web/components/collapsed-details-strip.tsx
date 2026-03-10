'use client'

import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { FileText, Image as ImageIcon, FileDown } from 'lucide-react'
import type { SlideDetail } from '@/lib/types'

interface CollapsedDetailsStripProps {
  slide: SlideDetail | null
  onOpenTab: (tab: string) => void
}

export function CollapsedDetailsStrip({ slide, onOpenTab }: CollapsedDetailsStripProps) {
  const figuresCount = slide?.figures.length ?? 0
  const filesCount = slide?.data_files.length ?? 0

  return (
    <div className="h-full w-12 bg-card border-l border-border flex flex-col items-center py-3 gap-1">
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => onOpenTab('notes')}
            className="w-9 h-9"
          >
            <FileText className="w-4 h-4" />
          </Button>
        </TooltipTrigger>
        <TooltipContent side="left">Notes</TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => onOpenTab('figures')}
            className="w-9 h-9 relative"
          >
            <ImageIcon className="w-4 h-4" />
            {figuresCount > 0 && (
              <span className="absolute top-1 left-1/2 h-3.5 min-w-[14px] px-0.5 text-[9px] font-medium bg-muted/90 text-muted-foreground rounded-full flex items-center justify-center">
                {figuresCount}
              </span>
            )}
          </Button>
        </TooltipTrigger>
        <TooltipContent side="left">
          Figures{figuresCount > 0 && ` (${figuresCount})`}
        </TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => onOpenTab('files')}
            className="w-9 h-9 relative"
          >
            <FileDown className="w-4 h-4" />
            {filesCount > 0 && (
              <span className="absolute top-1 left-1/2 h-3.5 min-w-[14px] px-0.5 text-[9px] font-medium bg-muted/90 text-muted-foreground rounded-full flex items-center justify-center">
                {filesCount}
              </span>
            )}
          </Button>
        </TooltipTrigger>
        <TooltipContent side="left">
          Files{filesCount > 0 && ` (${filesCount})`}
        </TooltipContent>
      </Tooltip>
    </div>
  )
}
