import { createFileRoute } from '@tanstack/react-router'
import { ImageLibrary } from '@/features/images'

export const Route = createFileRoute('/_authenticated/images')({
  component: ImageLibrary,
})
