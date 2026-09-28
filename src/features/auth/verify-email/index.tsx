import { useEffect, useState } from 'react'
import { Link, useSearch } from '@tanstack/react-router'
import { CheckCircle2, Loader2, XCircle } from 'lucide-react'
import { verifyEmail } from '@/lib/imagehub-api'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { AuthLayout } from '../auth-layout'

export function VerifyEmail() {
  const { token } = useSearch({ from: '/(auth)/verify-email' })
  const [state, setState] = useState<'loading' | 'success' | 'error'>(token ? 'loading' : 'error')
  const [message, setMessage] = useState(token ? 'Verifying your email…' : 'The verification link is missing.')
  useEffect(() => {
    if (!token) return
    verifyEmail(token).then(() => { setState('success'); setMessage('Your email is verified. You can now sign in.') }).catch((error) => { setState('error'); setMessage(error instanceof Error ? error.message : 'The verification link is invalid or expired.') })
  }, [token])
  return <AuthLayout><Card className='max-w-sm gap-4'><CardHeader><CardTitle className='flex items-center gap-2 text-lg tracking-tight'>{state === 'loading' && <Loader2 className='animate-spin' />}{state === 'success' && <CheckCircle2 className='text-primary' />}{state === 'error' && <XCircle className='text-destructive' />}Email verification</CardTitle><CardDescription>{message}</CardDescription></CardHeader><CardContent><Button asChild className='w-full'><Link to='/sign-in'>Continue to sign in</Link></Button></CardContent></Card></AuthLayout>
}
