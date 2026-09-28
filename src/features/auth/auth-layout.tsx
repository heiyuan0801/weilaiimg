import { useEffect, useState } from 'react'
import { Logo } from '@/assets/logo'
import { getPublicSiteConfig } from '@/lib/imagehub-api'

type AuthLayoutProps = {
  children: React.ReactNode
}

export function AuthLayout({ children }: AuthLayoutProps) {
  const [site, setSite] = useState({ site_name: 'ImageHub', logo_url: '' })
  useEffect(() => { void getPublicSiteConfig().then((config) => setSite({ site_name: config.site_name || 'ImageHub', logo_url: config.logo_url || '' })).catch(() => undefined) }, [])
  return (
    <div className='container grid h-svh max-w-none items-center justify-center'>
      <div className='mx-auto flex w-full flex-col justify-center space-y-2 py-8 sm:p-8'>
        <div className='mb-4 flex items-center justify-center'>
          {site.logo_url ? <img src={site.logo_url} alt='' className='me-2 size-8 object-contain' /> : <Logo className='me-2' />}
          <h1 className='text-xl font-medium'>{site.site_name}</h1>
        </div>
        {children}
      </div>
    </div>
  )
}
