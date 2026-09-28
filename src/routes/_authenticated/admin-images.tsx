import { createFileRoute } from '@tanstack/react-router'
import { AdminMediaPage } from '@/features/admin-media'

export const Route = createFileRoute('/_authenticated/admin-images')({
  component: AdminMediaPage,
})
