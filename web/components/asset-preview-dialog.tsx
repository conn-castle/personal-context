'use client'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { FileDown, Trash2, X, FileText, Image as ImageIcon, File } from 'lucide-react'

interface AssetPreviewDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  filename: string
  type: 'figure' | 'file'
  description?: string | null
  size?: string
  onDownload?: () => void
  onDelete?: () => void
}

function getFileType(filename: string): 'image' | 'text' | 'other' {
  const ext = filename.split('.').pop()?.toLowerCase() || ''
  const imageExtensions = ['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico']
  const textExtensions = ['md', 'markdown', 'txt', 'text', 'json', 'xml', 'html', 'css', 'js', 'ts', 'tsx', 'jsx']
  
  if (imageExtensions.includes(ext)) return 'image'
  if (textExtensions.includes(ext)) return 'text'
  return 'other'
}

function getFileExtension(filename: string): string {
  return filename.split('.').pop()?.toUpperCase() || 'FILE'
}

export function AssetPreviewDialog(props: AssetPreviewDialogProps) {
  const { open, onOpenChange, filename, description, size, onDownload, onDelete } = props
  const fileType = getFileType(filename)
  const extension = getFileExtension(filename)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent 
        className="max-w-3xl max-h-[85vh] flex flex-col"
        showCloseButton={false}
      >
        {/* Custom header with toolbar */}
        <DialogHeader className="flex-row items-center justify-between gap-4 space-y-0 border-b border-border pb-4">
          <div className="min-w-0 flex-1">
            <DialogTitle className="truncate">{filename}</DialogTitle>
            {(description || size) && (
              <p className="text-sm text-muted-foreground mt-1 truncate">
                {size && <span>{size}</span>}
                {size && description && <span className="mx-1.5">·</span>}
                {description && <span>{description}</span>}
              </p>
            )}
          </div>
          
          {/* Toolbar */}
          <div className="flex items-center gap-1 flex-shrink-0">
            {onDownload && (
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={onDownload}
                title="Download"
              >
                <FileDown className="w-4 h-4" />
              </Button>
            )}
            {onDelete && (
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={onDelete}
                title="Delete"
                className="text-destructive hover:text-destructive"
              >
                <Trash2 className="w-4 h-4" />
              </Button>
            )}
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => onOpenChange(false)}
              title="Close"
            >
              <X className="w-4 h-4" />
            </Button>
          </div>
        </DialogHeader>

        {/* Preview content */}
        <div className="flex-1 min-h-0 overflow-auto">
          {fileType === 'image' ? (
            <div className="flex items-center justify-center p-4 bg-muted/30 rounded-lg min-h-[300px]">
              {/* In a real app, this would show the actual image */}
              <div className="text-center text-muted-foreground">
                <ImageIcon className="w-16 h-16 mx-auto mb-3 opacity-50" />
                <p className="text-sm">Image preview</p>
                <p className="text-xs mt-1 opacity-70">
                  (In production, the actual image would be displayed here)
                </p>
              </div>
            </div>
          ) : fileType === 'text' ? (
            <div className="p-4 bg-muted/30 rounded-lg min-h-[300px]">
              {/* In a real app, this would show the actual text content */}
              <div className="text-center text-muted-foreground py-12">
                <FileText className="w-16 h-16 mx-auto mb-3 opacity-50" />
                <p className="text-sm">Text preview</p>
                <p className="text-xs mt-1 opacity-70">
                  (In production, the actual content would be displayed here)
                </p>
              </div>
            </div>
          ) : (
            <div className="flex items-center justify-center p-8 bg-muted/30 rounded-lg min-h-[200px]">
              <div className="text-center">
                <div className="relative w-20 h-20 mx-auto mb-4">
                  <File className="w-20 h-20 text-muted-foreground/50" />
                  <span className="absolute bottom-2 left-1/2 -translate-x-1/2 text-[10px] font-semibold text-muted-foreground">
                    {extension}
                  </span>
                </div>
                <p className="text-sm text-muted-foreground">No preview available</p>
                <p className="text-xs text-muted-foreground/70 mt-1">
                  Download to view this file
                </p>
              </div>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
