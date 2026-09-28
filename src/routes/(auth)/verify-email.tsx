import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { VerifyEmail } from '@/features/auth/verify-email'

const searchSchema = z.object({ token: z.string().optional().catch('') })

export const Route = createFileRoute('/(auth)/verify-email')({
  validateSearch: searchSchema,
  component: VerifyEmail,
})
