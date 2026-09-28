import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/clerk/_authenticated')({
  beforeLoad: () => { throw redirect({ to: '/' }) },
})
