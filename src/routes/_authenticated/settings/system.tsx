import { createFileRoute } from '@tanstack/react-router'
import { SystemSettingsPage } from '@/features/settings/system'

export const Route = createFileRoute('/_authenticated/settings/system')({
  component: SystemSettingsPage,
})
