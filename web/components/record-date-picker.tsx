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
import type { RecordSummary } from '@/lib/types'

interface RecordDatePickerProps {
  records: RecordSummary[]
  onSelectDate: (date: Date) => void
}

export function RecordDatePicker({ records, onSelectDate }: RecordDatePickerProps) {
  const [open, setOpen] = useState(false)
  const [inputValue, setInputValue] = useState('')
  const [inputError, setInputError] = useState(false)

  // Get all unique dates that have records
  const recordDates = useMemo(() => {
    const dates = new Set<string>()
    for (const record of records) {
      dates.add(record.date)
    }
    return dates
  }, [records])

  // Convert to Date objects for the calendar
  const datesWithRecords = useMemo(() => {
    return Array.from(recordDates).map(dateStr => new Date(dateStr + 'T00:00:00'))
  }, [recordDates])

  // Get the date range for the calendar
  const dateRange = useMemo(() => {
    if (datesWithRecords.length === 0) {
      return { from: new Date(), to: new Date() }
    }
    const sorted = [...datesWithRecords].sort((a, b) => a.getTime() - b.getTime())
    return { from: sorted[0], to: sorted[sorted.length - 1] }
  }, [datesWithRecords])

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

  // Custom modifiers to highlight dates with records
  const modifiers = {
    hasRecords: datesWithRecords,
  }

  const modifiersStyles = {
    hasRecords: {
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
            Or select from calendar. Bold dates have records.
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
            return !recordDates.has(dateStr)
          }}
        />
      </PopoverContent>
    </Popover>
  )
}
