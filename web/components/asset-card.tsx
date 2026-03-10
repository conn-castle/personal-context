'use client'

import { useState } from 'react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { FileDown, Image as ImageIcon } from 'lucide-react'
import { AssetPreviewDialog } from '@/components/asset-preview-dialog'

interface AssetCardProps {
  type: 'figure' | 'file'
  filename: string
  description?: string | null
  size?: string
  onDownload?: () => void
  onDelete?: () => void
}

export function AssetCard({
  type,
  filename,
  description,
  size,
  onDownload,
  onDelete,
}: AssetCardProps) {
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)
  const DefaultIcon = type === 'figure' ? ImageIcon : FileDown

  const handleDelete = () => {
    setDeleteConfirmOpen(false)
    onDelete?.()
  }

  return (
    <>
      <div 
        className="p-3 rounded-lg bg-muted/50 border border-border hover:border-primary/30 hover:bg-muted/70 transition-colors cursor-pointer"
        onClick={() => setDialogOpen(true)}
      >
        <div className="flex items-start gap-3">
          {/* Icon */}
          <div className="w-9 h-9 rounded bg-muted flex items-center justify-center flex-shrink-0">
            <DefaultIcon className="w-4 h-4 text-muted-foreground" />
          </div>

          {/* Content */}
          <div className="flex-1 min-w-0">
            <p className="text-sm font-medium leading-tight break-words">
              {filename}
            </p>
            
            {/* Meta row: size and/or description */}
            {(size || description) && (
              <div className="mt-1 text-xs text-muted-foreground">
                {size && <span className="inline">{size}</span>}
                {size && description && <span className="mx-1.5">·</span>}
                {description && (
                  <span className="break-words">{description}</span>
                )}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* File details / preview dialog */}
      <AssetPreviewDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        filename={filename}
        type={type}
        description={description}
        size={size}
        onDownload={onDownload}
        onDelete={() => {
          setDialogOpen(false)
          setDeleteConfirmOpen(true)
        }}
      />

      {/* Delete confirmation dialog */}
      <AlertDialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {type === 'figure' ? 'figure' : 'file'}?</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete &quot;{filename}&quot;? This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction 
              onClick={handleDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
