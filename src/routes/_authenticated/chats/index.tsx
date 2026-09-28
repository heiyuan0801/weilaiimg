import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/chats/')({
  beforeLoad: () => { throw redirect({ to: '/' }) },
})
