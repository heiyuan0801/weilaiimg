import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { ResetPassword } from '@/features/auth/reset-password'

const searchSchema = z.object({ token: z.string().optional().catch('') })

export const Route = createFileRoute('/(auth)/reset-password')({
  validateSearch: searchSchema,
  component: ResetPassword,
})
