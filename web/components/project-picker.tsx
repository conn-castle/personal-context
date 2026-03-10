'use client'

import { useState, useMemo } from 'react'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { FolderOpen, ChevronDown, X, Check } from 'lucide-react'

interface ProjectPickerProps {
  projects: string[]
  selectedProjects: string[]
  onSelectionChange: (projects: string[]) => void
}

export function ProjectPicker({
  projects,
  selectedProjects,
  onSelectionChange,
}: ProjectPickerProps) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')

  const filteredProjects = useMemo(() => {
    if (!search) return projects
    const lowerSearch = search.toLowerCase()
    return projects.filter((p) => p.toLowerCase().includes(lowerSearch))
  }, [projects, search])

  const isAllSelected = selectedProjects.length === projects.length
  const hasSelection = selectedProjects.length > 0

  const toggleProject = (project: string) => {
    if (selectedProjects.includes(project)) {
      onSelectionChange(selectedProjects.filter((p) => p !== project))
    } else {
      onSelectionChange([...selectedProjects, project])
    }
  }

  const selectAll = () => {
    onSelectionChange([...projects])
  }

  const clearAll = () => {
    onSelectionChange([])
  }

  const getDisplayText = () => {
    if (!hasSelection || isAllSelected) {
      return 'All Projects'
    }
    if (selectedProjects.length === 1) {
      return selectedProjects[0]
    }
    return `${selectedProjects.length} projects`
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 gap-1.5 px-2 text-xs font-normal"
        >
          <FolderOpen className="w-3.5 h-3.5 text-muted-foreground" />
          <span className="max-w-[150px] truncate">{getDisplayText()}</span>
          <ChevronDown className="w-3 h-3 text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-72 p-0" align="start">
        <Command shouldFilter={false}>
          <CommandInput
            placeholder="Search projects..."
            value={search}
            onValueChange={setSearch}
          />
          <CommandList>
            <CommandEmpty>No projects found.</CommandEmpty>
            <CommandGroup>
              {/* Select All / Clear All */}
              <CommandItem
                onSelect={isAllSelected ? clearAll : selectAll}
                className="justify-between data-[selected=true]:bg-accent/10"
              >
                <div className="flex items-center gap-2">
                  <div className={cn(
                    "flex h-4 w-4 items-center justify-center rounded-sm border border-input",
                    isAllSelected && "bg-foreground text-background"
                  )}>
                    {isAllSelected && <Check className="h-3 w-3" />}
                  </div>
                  <span className="font-medium">
                    {isAllSelected ? 'Clear All' : 'Select All'}
                  </span>
                </div>
                <span className="text-xs text-muted-foreground">
                  {projects.length} total
                </span>
              </CommandItem>
            </CommandGroup>
            <CommandSeparator />
            <CommandGroup className="max-h-[200px] overflow-y-auto">
              {filteredProjects.map((project) => {
                const isSelected = selectedProjects.includes(project)
                return (
                  <CommandItem
                    key={project}
                    onSelect={() => toggleProject(project)}
                    className="gap-2 data-[selected=true]:bg-accent/10"
                  >
                    <div className={cn(
                      "flex h-4 w-4 items-center justify-center rounded-sm border border-input",
                      isSelected && "bg-foreground text-background"
                    )}>
                      {isSelected && <Check className="h-3 w-3" />}
                    </div>
                    <span className="truncate font-mono text-xs">{project}</span>
                  </CommandItem>
                )
              })}
            </CommandGroup>
          </CommandList>
        </Command>

        {/* Selected projects badges */}
        {hasSelection && !isAllSelected && (
          <>
            <div className="border-t border-border p-2">
              <div className="flex flex-wrap gap-1">
                {selectedProjects.slice(0, 3).map((project) => (
                  <Badge
                    key={project}
                    variant="secondary"
                    className="text-xs font-mono gap-1 pr-1"
                  >
                    <span className="truncate max-w-[100px]">{project}</span>
                    <button
                      onClick={(e) => {
                        e.stopPropagation()
                        toggleProject(project)
                      }}
                      className="ml-0.5 rounded-sm hover:bg-muted p-0.5"
                    >
                      <X className="w-3 h-3" />
                    </button>
                  </Badge>
                ))}
                {selectedProjects.length > 3 && (
                  <Badge variant="outline" className="text-xs">
                    +{selectedProjects.length - 3} more
                  </Badge>
                )}
              </div>
            </div>
          </>
        )}
      </PopoverContent>
    </Popover>
  )
}
