'use client'

import { useState } from 'react'
import { useTheme } from 'next-themes'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Checkbox } from '@/components/ui/checkbox'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { ScrollArea } from '@/components/ui/scroll-area'
import { X, Github, Globe, Users, Trash2, Plus } from 'lucide-react'

interface SettingsOverlayProps {
  open: boolean
  onClose: () => void
}

export function SettingsOverlay({ open, onClose }: SettingsOverlayProps) {
  // Demo state for various input types
  const [autoSave, setAutoSave] = useState(true)
  const [showThumbnails, setShowThumbnails] = useState(true)
  const [compactMode, setCompactMode] = useState(false)
  const { theme, setTheme } = useTheme()
  const [defaultView, setDefaultView] = useState('strip')
  const [slideBackground, setSlideBackground] = useState('default')
  const [fontSize, setFontSize] = useState('medium')
  const [shareAccess, setShareAccess] = useState('private')
  const [allowComments, setAllowComments] = useState(true)
  const [allowDownloads, setAllowDownloads] = useState(false)
  const [notifyOnView, setNotifyOnView] = useState(true)
  const [displayName, setDisplayName] = useState('My Research Project')
  const [description, setDescription] = useState('')
  const [linkedRepos, setLinkedRepos] = useState([
    { id: '1', name: 'happy-ai/sleep-staging-classifier', branch: 'main' },
    { id: '2', name: 'happy-ai/data-pipeline', branch: 'develop' },
  ])
  const [selectedTags, setSelectedTags] = useState(['research', 'ml'])
  const [exportFormats, setExportFormats] = useState({
    pdf: true,
    html: true,
    markdown: false,
    json: false,
  })

  const availableTags = ['research', 'ml', 'production', 'draft', 'archived', 'shared']

  const toggleTag = (tag: string) => {
    setSelectedTags(prev => 
      prev.includes(tag) 
        ? prev.filter(t => t !== tag)
        : [...prev, tag]
    )
  }

  const removeRepo = (id: string) => {
    setLinkedRepos(prev => prev.filter(r => r.id !== id))
  }

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 bg-background">
      {/* Header */}
      <header className="h-14 border-b border-border flex items-center justify-between px-6">
        <h1 className="text-lg font-semibold">Settings</h1>
        <Button variant="ghost" size="icon-sm" onClick={onClose}>
          <X className="w-5 h-5" />
        </Button>
      </header>

      {/* Content */}
      <ScrollArea className="h-[calc(100vh-3.5rem)]">
        <div className="max-w-2xl mx-auto p-6 pb-12">
          <div className="space-y-10">
            
            {/* Project Info Section */}
            <section>
              <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-4">
                Project Info
              </h2>
              <div className="space-y-4">
                {/* Text Input */}
                <div className="space-y-2">
                  <Label htmlFor="displayName">Display Name</Label>
                  <Input 
                    id="displayName"
                    value={displayName}
                    onChange={(e) => setDisplayName(e.target.value)}
                    placeholder="Enter project name"
                  />
                </div>
                
                {/* Textarea */}
                <div className="space-y-2">
                  <Label htmlFor="description">Description</Label>
                  <Textarea 
                    id="description"
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    placeholder="Add a description for this project..."
                    rows={3}
                  />
                  <p className="text-xs text-muted-foreground">
                    This description will be visible to anyone with access.
                  </p>
                </div>

                {/* Multi-select with badges (Tags) */}
                <div className="space-y-2">
                  <Label>Tags</Label>
                  <div className="flex flex-wrap gap-2">
                    {availableTags.map(tag => (
                      <Badge 
                        key={tag}
                        variant={selectedTags.includes(tag) ? 'default' : 'outline'}
                        className="cursor-pointer"
                        onClick={() => toggleTag(tag)}
                      >
                        {tag}
                      </Badge>
                    ))}
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Click to select or deselect tags.
                  </p>
                </div>
              </div>
            </section>

            {/* General Section */}
            <section>
              <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-4">
                General
              </h2>
              <div className="space-y-1">
                {/* Toggle/Switch */}
                <div className="flex items-center justify-between py-3 border-b border-border">
                  <div>
                    <p className="text-sm font-medium">Auto-save notes</p>
                    <p className="text-xs text-muted-foreground">Automatically save notes as you type</p>
                  </div>
                  <Switch checked={autoSave} onCheckedChange={setAutoSave} />
                </div>
                
                {/* Toggle/Switch */}
                <div className="flex items-center justify-between py-3 border-b border-border">
                  <div>
                    <p className="text-sm font-medium">Show thumbnails</p>
                    <p className="text-xs text-muted-foreground">Display slide thumbnails in navigation</p>
                  </div>
                  <Switch checked={showThumbnails} onCheckedChange={setShowThumbnails} />
                </div>

                {/* Toggle/Switch */}
                <div className="flex items-center justify-between py-3 border-b border-border">
                  <div>
                    <p className="text-sm font-medium">Compact mode</p>
                    <p className="text-xs text-muted-foreground">Reduce spacing for more content density</p>
                  </div>
                  <Switch checked={compactMode} onCheckedChange={setCompactMode} />
                </div>

                {/* Dropdown/Select */}
                <div className="flex items-center justify-between py-3 border-b border-border">
                  <div>
                    <p className="text-sm font-medium">Default view mode</p>
                    <p className="text-xs text-muted-foreground">How slides appear in the navigation panel</p>
                  </div>
                  <Select value={defaultView} onValueChange={setDefaultView}>
                    <SelectTrigger className="w-[140px]">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="strip">Strip view</SelectItem>
                      <SelectItem value="grid">Grid view</SelectItem>
                      <SelectItem value="list">List view</SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                {/* Dropdown/Select */}
                <div className="flex items-center justify-between py-3 border-b border-border">
                  <div>
                    <p className="text-sm font-medium">Font size</p>
                    <p className="text-xs text-muted-foreground">Base font size for the interface</p>
                  </div>
                  <Select value={fontSize} onValueChange={setFontSize}>
                    <SelectTrigger className="w-[140px]">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="small">Small</SelectItem>
                      <SelectItem value="medium">Medium</SelectItem>
                      <SelectItem value="large">Large</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>
            </section>

            {/* Appearance Section */}
            <section>
              <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-4">
                Appearance
              </h2>
              <div className="space-y-1">
                {/* Dropdown/Select */}
                <div className="flex items-center justify-between py-3 border-b border-border">
                  <div>
                    <p className="text-sm font-medium">Theme</p>
                    <p className="text-xs text-muted-foreground">Select your preferred color scheme</p>
                  </div>
                  <Select value={theme} onValueChange={setTheme}>
                    <SelectTrigger className="w-[140px]">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="light">Light</SelectItem>
                      <SelectItem value="dark">Dark</SelectItem>
                      <SelectItem value="system">System</SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                {/* Dropdown/Select */}
                <div className="flex items-center justify-between py-3 border-b border-border">
                  <div>
                    <p className="text-sm font-medium">Slide background</p>
                    <p className="text-xs text-muted-foreground">Background color for the main slide view</p>
                  </div>
                  <Select value={slideBackground} onValueChange={setSlideBackground}>
                    <SelectTrigger className="w-[140px]">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="default">Default</SelectItem>
                      <SelectItem value="white">White</SelectItem>
                      <SelectItem value="gray">Gray</SelectItem>
                      <SelectItem value="dark">Dark</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>
            </section>

            {/* Sharing & Privacy Section */}
            <section>
              <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-4">
                Sharing & Privacy
              </h2>
              <div className="space-y-1">
                {/* Dropdown/Select */}
                <div className="flex items-center justify-between py-3 border-b border-border">
                  <div className="flex items-start gap-3">
                    <Globe className="w-4 h-4 mt-0.5 text-muted-foreground" />
                    <div>
                      <p className="text-sm font-medium">Access level</p>
                      <p className="text-xs text-muted-foreground">Who can view this project</p>
                    </div>
                  </div>
                  <Select value={shareAccess} onValueChange={setShareAccess}>
                    <SelectTrigger className="w-[160px]">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="private">Private (only me)</SelectItem>
                      <SelectItem value="team">Team members</SelectItem>
                      <SelectItem value="link">Anyone with link</SelectItem>
                      <SelectItem value="public">Public</SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                {/* Toggle/Switch with icon */}
                <div className="flex items-center justify-between py-3 border-b border-border">
                  <div className="flex items-start gap-3">
                    <Users className="w-4 h-4 mt-0.5 text-muted-foreground" />
                    <div>
                      <p className="text-sm font-medium">Allow comments</p>
                      <p className="text-xs text-muted-foreground">Let viewers add comments to slides</p>
                    </div>
                  </div>
                  <Switch checked={allowComments} onCheckedChange={setAllowComments} />
                </div>

                {/* Toggle/Switch */}
                <div className="flex items-center justify-between py-3 border-b border-border">
                  <div>
                    <p className="text-sm font-medium">Allow downloads</p>
                    <p className="text-xs text-muted-foreground">Let viewers download slides and files</p>
                  </div>
                  <Switch checked={allowDownloads} onCheckedChange={setAllowDownloads} />
                </div>

                {/* Toggle/Switch */}
                <div className="flex items-center justify-between py-3 border-b border-border">
                  <div>
                    <p className="text-sm font-medium">Notify on view</p>
                    <p className="text-xs text-muted-foreground">Get notified when someone views this project</p>
                  </div>
                  <Switch checked={notifyOnView} onCheckedChange={setNotifyOnView} />
                </div>
              </div>
            </section>

            {/* Linked Repositories Section */}
            <section>
              <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-4">
                Linked Repositories
              </h2>
              <div className="space-y-3">
                {linkedRepos.map(repo => (
                  <div 
                    key={repo.id}
                    className="flex items-center justify-between p-3 rounded-lg border border-border bg-muted/30"
                  >
                    <div className="flex items-center gap-3">
                      <Github className="w-4 h-4 text-muted-foreground" />
                      <div>
                        <p className="text-sm font-medium">{repo.name}</p>
                        <p className="text-xs text-muted-foreground">Branch: {repo.branch}</p>
                      </div>
                    </div>
                    <Button 
                      variant="ghost" 
                      size="icon-sm"
                      onClick={() => removeRepo(repo.id)}
                      className="text-muted-foreground hover:text-destructive"
                    >
                      <Trash2 className="w-4 h-4" />
                    </Button>
                  </div>
                ))}
                <Button variant="outline" size="sm" className="w-full">
                  <Plus className="w-4 h-4 mr-2" />
                  Link Repository
                </Button>
              </div>
            </section>

            {/* Export Options Section - Checkboxes */}
            <section>
              <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-4">
                Export Options
              </h2>
              <p className="text-sm text-muted-foreground mb-4">
                Select which formats are available when exporting slides.
              </p>
              <div className="space-y-3">
                <div className="flex items-center gap-3">
                  <Checkbox 
                    id="export-pdf"
                    checked={exportFormats.pdf}
                    onCheckedChange={(checked) => setExportFormats(prev => ({ ...prev, pdf: !!checked }))}
                  />
                  <Label htmlFor="export-pdf" className="text-sm font-normal cursor-pointer">
                    PDF Document (.pdf)
                  </Label>
                </div>
                <div className="flex items-center gap-3">
                  <Checkbox 
                    id="export-html"
                    checked={exportFormats.html}
                    onCheckedChange={(checked) => setExportFormats(prev => ({ ...prev, html: !!checked }))}
                  />
                  <Label htmlFor="export-html" className="text-sm font-normal cursor-pointer">
                    HTML Presentation (.html)
                  </Label>
                </div>
                <div className="flex items-center gap-3">
                  <Checkbox 
                    id="export-markdown"
                    checked={exportFormats.markdown}
                    onCheckedChange={(checked) => setExportFormats(prev => ({ ...prev, markdown: !!checked }))}
                  />
                  <Label htmlFor="export-markdown" className="text-sm font-normal cursor-pointer">
                    Markdown (.md)
                  </Label>
                </div>
                <div className="flex items-center gap-3">
                  <Checkbox 
                    id="export-json"
                    checked={exportFormats.json}
                    onCheckedChange={(checked) => setExportFormats(prev => ({ ...prev, json: !!checked }))}
                  />
                  <Label htmlFor="export-json" className="text-sm font-normal cursor-pointer">
                    JSON Data (.json)
                  </Label>
                </div>
              </div>
            </section>

            {/* Keyboard Shortcuts Section */}
            <section>
              <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-4">
                Keyboard Shortcuts
              </h2>
              <div className="space-y-2">
                <div className="flex items-center justify-between py-2">
                  <span className="text-sm">Navigate slides</span>
                  <div className="flex gap-1">
                    <kbd className="inline-flex items-center justify-center min-w-[24px] h-6 px-2 bg-muted text-muted-foreground border border-border rounded text-xs font-mono">←</kbd>
                    <kbd className="inline-flex items-center justify-center min-w-[24px] h-6 px-2 bg-muted text-muted-foreground border border-border rounded text-xs font-mono">→</kbd>
                  </div>
                </div>
                <div className="flex items-center justify-between py-2">
                  <span className="text-sm">Toggle navigation panel</span>
                  <kbd className="inline-flex items-center justify-center min-w-[24px] h-6 px-2 bg-muted text-muted-foreground border border-border rounded text-xs font-mono">[</kbd>
                </div>
                <div className="flex items-center justify-between py-2">
                  <span className="text-sm">Toggle metadata bar</span>
                  <kbd className="inline-flex items-center justify-center min-w-[24px] h-6 px-2 bg-muted text-muted-foreground border border-border rounded text-xs font-mono">\</kbd>
                </div>
                <div className="flex items-center justify-between py-2">
                  <span className="text-sm">Toggle details panel</span>
                  <kbd className="inline-flex items-center justify-center min-w-[24px] h-6 px-2 bg-muted text-muted-foreground border border-border rounded text-xs font-mono">]</kbd>
                </div>
              </div>
            </section>

            {/* Danger Zone Section */}
            <section>
              <h2 className="text-sm font-semibold text-destructive uppercase tracking-wider mb-4">
                Danger Zone
              </h2>
              <div className="p-4 border border-destructive/30 rounded-lg bg-destructive/5 space-y-4">
                <div className="flex items-center justify-between">
                  <div>
                    <p className="text-sm font-medium">Delete this project</p>
                    <p className="text-xs text-muted-foreground">
                      Once deleted, this project cannot be recovered.
                    </p>
                  </div>
                  <Button variant="destructive" size="sm">
                    Delete Project
                  </Button>
                </div>
              </div>
            </section>

            {/* About Section */}
            <section>
              <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-4">
                About
              </h2>
              <div className="space-y-2 text-sm text-muted-foreground">
                <p>Personal Context Viewer</p>
                <p>Version 1.0.0</p>
              </div>
            </section>

          </div>
        </div>
      </ScrollArea>
    </div>
  )
}
