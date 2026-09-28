import { useState } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2, LockKeyhole } from 'lucide-react'
import { toast } from 'sonner'
import { resetPassword } from '@/lib/imagehub-api'
import { Button } from '@/components/ui/button'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { PasswordInput } from '@/components/password-input'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { AuthLayout } from '../auth-layout'

const formSchema = z.object({
  password: z.string().min(10, 'Password must be at least 10 characters long.'),
  confirmPassword: z.string().min(1, 'Please confirm your password.'),
}).refine((data) => data.password === data.confirmPassword, { message: "Passwords don't match.", path: ['confirmPassword'] })

export function ResetPassword() {
  const { token } = useSearch({ from: '/(auth)/reset-password' })
  const navigate = useNavigate()
  const [isLoading, setIsLoading] = useState(false)
  const form = useForm<z.infer<typeof formSchema>>({ resolver: zodResolver(formSchema), defaultValues: { password: '', confirmPassword: '' } })

  async function onSubmit(data: z.infer<typeof formSchema>) {
    if (!token) { toast.error('The reset link is missing or invalid.'); return }
    setIsLoading(true)
    try {
      await resetPassword({ token, password: data.password })
      toast.success('Password reset. You can now sign in.')
      navigate({ to: '/sign-in', replace: true })
    } catch (error) { toast.error(error instanceof Error ? error.message : 'Could not reset password') }
    finally { setIsLoading(false) }
  }

  return <AuthLayout><Card className='max-w-sm gap-4'><CardHeader><CardTitle className='text-lg tracking-tight'>Reset password</CardTitle><CardDescription>Choose a new password for your ImageHub account.</CardDescription></CardHeader><CardContent><Form {...form}><form onSubmit={form.handleSubmit(onSubmit)} className='grid gap-3'><FormField control={form.control} name='password' render={({ field }) => <FormItem><FormLabel>New password</FormLabel><FormControl><PasswordInput placeholder='********' {...field} /></FormControl><FormMessage /></FormItem>} /><FormField control={form.control} name='confirmPassword' render={({ field }) => <FormItem><FormLabel>Confirm password</FormLabel><FormControl><PasswordInput placeholder='********' {...field} /></FormControl><FormMessage /></FormItem>} /><Button className='mt-2' disabled={isLoading || !token}>{isLoading ? <Loader2 className='animate-spin' /> : <LockKeyhole />}Reset password</Button></form></Form></CardContent></Card></AuthLayout>
}
