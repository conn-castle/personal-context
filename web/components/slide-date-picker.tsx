'use client'

import { useState, useMemo } from 'react'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { CalendarDays } from 'lucide-react'
import type { SlideSummary } from '@/lib/types'

interface SlideDatePickerProps {
  slides: SlideSummary[]
  onSelectDate: (date: Date) => void
}

export function SlideDatePicker({ slides, onSelectDate }: SlideDatePickerProps) {
  const [open, setOpen] = useState(false)
  const [inputValue, setInputValue] = useState('')
  const [inputError, setInputError] = useState(false)

  // Get all unique dates that have slides
  const slideDates = useMemo(() => {
    const dates = new Set<string>()
    for (const slide of slides) {
      dates.add(slide.date)
    }
    return dates
  }, [slides])

  // Convert to Date objects for the calendar
  const datesWithSlides = useMemo(() => {
    return Array.from(slideDates).map(dateStr => new Date(dateStr + 'T00:00:00'))
  }, [slideDates])

  // Get the date range for the calendar
  const dateRange = useMemo(() => {
    if (datesWithSlides.length === 0) {
      return { from: new Date(), to: new Date() }
    }
    const sorted = [...datesWithSlides].sort((a, b) => a.getTime() - b.getTime())
    return { from: sorted[0], to: sorted[sorted.length - 1] }
  }, [datesWithSlides])

  const handleSelect = (date: Date | undefined) => {
    if (date) {
      onSelectDate(date)
      setOpen(false)
      setInputValue('')
      setInputError(false)
    }
  }

  const handleInputSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!inputValue.trim()) return

    // Try to parse the input as a date
    const parsed = new Date(inputValue)
    if (isNaN(parsed.getTime())) {
      setInputError(true)
      return
    }

    setInputError(false)
    onSelectDate(parsed)
    setOpen(false)
    setInputValue('')
  }

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setInputValue(e.target.value)
    setInputError(false)
  }

  // Custom modifiers to highlight dates with slides
  const modifiers = {
    hasSlides: datesWithSlides,
  }

  const modifiersStyles = {
    hasSlides: {
      fontWeight: 700,
    },
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>
            <Button variant="ghost" size="icon-sm">
              <CalendarDays className="w-4 h-4" />
            </Button>
          </PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent>Jump to date</TooltipContent>
      </Tooltip>
      <PopoverContent className="w-auto p-0" align="end">
        <div className="p-3 border-b border-border space-y-2">
          <form onSubmit={handleInputSubmit} className="flex gap-2">
            <Input
              type="text"
              placeholder="e.g. 2025-03-05"
              value={inputValue}
              onChange={handleInputChange}
              className={inputError ? 'border-destructive flex-1' : 'flex-1'}
            />
            <Button type="submit" size="sm" disabled={!inputValue.trim()}>
              Go
            </Button>
          </form>
          <p className="text-xs text-muted-foreground">
            Or select from calendar. Bold dates have slides.
          </p>
        </div>
        <Calendar
          mode="single"
          onSelect={handleSelect}
          defaultMonth={dateRange.to}
          modifiers={modifiers}
          modifiersStyles={modifiersStyles}
          disabled={(date) => {
            const dateStr = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
            return !slideDates.has(dateStr)
          }}
        />
      </PopoverContent>
    </Popover>
  )
}
