import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { buttonVariants } from '@/components/ui/button'

export function NotFound() {
  const { t } = useTranslation()
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 p-10 text-center">
      <h1 className="text-2xl font-semibold">{t('errors.notFoundTitle')}</h1>
      <p className="text-muted-foreground">{t('errors.notFoundBody')}</p>
      <Link to="/" className={buttonVariants({ variant: 'outline' })}>
        {t('nav.calendar')}
      </Link>
    </div>
  )
}
