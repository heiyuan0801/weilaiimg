import { useEffect, useState } from 'react'
import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Link, useNavigate } from '@tanstack/react-router'
import { Loader2, LogIn } from 'lucide-react'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { cn } from '@/lib/utils'
import { getCurrentUser, login, listPublicOIDCProviders, type PublicOIDCProvider } from '@/lib/imagehub-api'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import { PasswordInput } from '@/components/password-input'
import { useI18n } from '@/lib/i18n'

const formSchema = z.object({
  email: z.string().min(1, 'Please enter your email or username.'),
  password: z
    .string()
    .min(1, 'Please enter your password.')
    .min(10, 'Password must be at least 10 characters long.'),
  remember: z.boolean().default(false),
})

interface UserAuthFormProps extends React.HTMLAttributes<HTMLFormElement> {
  redirectTo?: string
}

function getSafeRedirect(value?: string) {
  if (!value) return '/dashboard'
  try {
    const target = new URL(value, window.location.origin)
    if (target.origin !== window.location.origin) return '/dashboard'
    return `${target.pathname}${target.search}${target.hash}` || '/dashboard'
  } catch {
    return value.startsWith('/') ? value : '/dashboard'
  }
}

export function UserAuthForm({
  className,
  redirectTo,
  ...props
}: UserAuthFormProps) {
  const { t } = useI18n()
  const [isLoading, setIsLoading] = useState(false)
  const [oidcProviders, setOIDCProviders] = useState<PublicOIDCProvider[]>([])
  const navigate = useNavigate()
  const { auth } = useAuthStore()
  useEffect(() => { void listPublicOIDCProviders().then(setOIDCProviders).catch(() => undefined) }, [])

  const form = useForm<z.input<typeof formSchema>, unknown, z.output<typeof formSchema>>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      email: '',
      password: '',
      remember: false,
    },
  })

  async function onSubmit(data: z.output<typeof formSchema>) {
    setIsLoading(true)
    try {
      const result = await login(data)
      // Verify that the browser retained the server session cookie before
      // navigating into a protected route. This turns a cookie/CORS problem
      // into a visible login error instead of a redirect loop.
      const currentUser = await getCurrentUser()
      auth.setUser({ accountNo: currentUser.id || result.user_id, email: currentUser.email || data.email, role: [currentUser.role || result.role], exp: Date.now() + 24 * 60 * 60 * 1000 })
      auth.setAccessToken('cookie-session')
      navigate({ to: getSafeRedirect(redirectTo), replace: true })
      toast.success(`Welcome back, ${data.email}!`)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Could not sign in')
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className={cn('grid gap-3', className)}
        {...props}
      >
        <FormField
          control={form.control}
          name='email'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('auth.emailOrUsername')}</FormLabel>
              <FormControl>
                <Input placeholder='name@example.com or username' {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='password'
          render={({ field }) => (
            <FormItem className='relative'>
              <FormLabel>{t('auth.password')}</FormLabel>
              <FormControl>
                <PasswordInput placeholder='********' {...field} />
              </FormControl>
              <FormMessage />
              <Link
                to='/forgot-password'
                className='absolute inset-e-0 -top-0.5 text-sm font-medium text-muted-foreground hover:opacity-75'
              >
                {t('auth.forgot')}
              </Link>
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='remember'
          render={({ field }) => <FormItem className='flex items-center gap-2 space-y-0'><FormControl><Checkbox checked={field.value} onCheckedChange={field.onChange} /></FormControl><FormLabel className='font-normal'>{t('auth.remember')}</FormLabel></FormItem>}
        />
        <Button className='mt-2' disabled={isLoading}>
          {isLoading ? <Loader2 className='animate-spin' /> : <LogIn />}
          {t('auth.signIn')}
        </Button>

        <div className='relative my-2'>
          <div className='absolute inset-0 flex items-center'>
            <span className='w-full border-t' />
          </div>
          <div className='relative flex justify-center text-xs uppercase'>
            <span className='bg-background px-2 text-muted-foreground'>
              {t('auth.continue')}
            </span>
          </div>
        </div>

        {oidcProviders.length > 0 && <div className='grid gap-2'>{oidcProviders.map((provider) => <Button key={provider.id} variant='outline' type='button' disabled={isLoading} onClick={() => { window.location.assign(`/api/v1/auth/oidc/${provider.id}/start`) }}>Continue with {provider.name}</Button>)}</div>}
      </form>
    </Form>
  )
}
