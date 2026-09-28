import { useEffect } from 'react'
import { getPublicSiteConfig } from '@/lib/imagehub-api'

function updateFavicon(url: string) {
  if (!url) return
  const links = Array.from(document.querySelectorAll<HTMLLinkElement>('link[rel~="icon"]'))
  const link = links[0] ?? document.head.appendChild(document.createElement('link'))
  link.rel = 'icon'
  link.href = url
  links.slice(1).forEach((item) => item.remove())
}

export function SiteConfigSync() {
  useEffect(() => {
    void getPublicSiteConfig().then((config) => {
      if (config.site_name) document.title = config.site_name
      updateFavicon(config.favicon_url)
    }).catch(() => undefined)
  }, [])
  return null
}
