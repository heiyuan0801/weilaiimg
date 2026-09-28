import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/help-center/')({
  beforeLoad: () => { throw redirect({ to: '/' }) },
})
