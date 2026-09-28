import { createFileRoute } from '@tanstack/react-router'
import { PublicHome } from '@/features/home'

export const Route = createFileRoute('/')({
  component: PublicHome,
})
