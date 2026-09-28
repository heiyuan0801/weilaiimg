import * as React from 'react'
import { cn } from '@/lib/utils'

export function Progress({ className, value = 0, ...props }: React.HTMLAttributes<HTMLDivElement> & { value?: number }) {
  return <div role='progressbar' aria-valuenow={value} className={cn('relative h-2 w-full overflow-hidden rounded-full bg-secondary', className)} {...props}><div className='h-full bg-primary transition-all' style={{ width: `${Math.min(100, Math.max(0, value))}%` }} /></div>
}
