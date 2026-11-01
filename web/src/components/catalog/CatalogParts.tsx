import { useTranslation } from 'react-i18next'
import type { CatalogKind, CatalogRequest, CatalogRequestStatus, TagSlot } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { TypeGlyph } from '@/lib/typeIcons'

/** A tag's dot or an event type's icon tile, in the item's theme color slot. */
export function CatalogSwatch({ kind, icon, slot }: { kind: CatalogKind; icon?: string; slot: TagSlot }) {
  if (kind === 'tag') return <span className="size-3 shrink-0 rounded-full" style={{ background: `var(--${slot})` }} aria-hidden />
  return (
    <span
      className="flex size-8 shrink-0 items-center justify-center rounded-lg"
      style={{ background: `var(--${slot})`, color: `var(--${slot}-foreground)` }}
      aria-hidden
    >
      <TypeGlyph name={icon || 'calendar'} className="size-4" />
    </span>
  )
}

const STATUS_VARIANT: Record<CatalogRequestStatus, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  open: 'secondary',
  approved: 'default',
  rejected: 'destructive',
  withdrawn: 'outline',
}

/** Status is always spelled out; color is only a hint. */
function RequestStatus({ status }: { status: CatalogRequestStatus }) {
  const { t } = useTranslation()
  return <Badge variant={STATUS_VARIANT[status]}>{t(`catalog.status.${status}`)}</Badge>
}

/** One request's name, kind and note, shared by the requester's and the admins' lists. */
export function RequestSummary({ request, byline }: { request: CatalogRequest; byline?: string }) {
  const { t } = useTranslation()
  return (
    <div className="flex min-w-0 flex-1 items-start gap-3">
      <div className="pt-1">
        <CatalogSwatch kind={request.kind} icon={request.icon} slot={request.colorSlot} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium break-words">{request.name}</span>
          <Badge variant="outline">{t(`catalog.kinds.${request.kind}`)}</Badge>
          <RequestStatus status={request.status} />
        </div>
        {byline ? <p className="mt-0.5 text-xs text-muted-foreground">{byline}</p> : null}
        {request.other ? <p className="mt-1 text-sm break-words whitespace-pre-wrap">{request.other}</p> : null}
        {request.reviewNote ? (
          <p className="mt-1 text-sm text-muted-foreground">
            <span className="font-medium">{t('catalog.reviewNote')}</span> {request.reviewNote}
          </p>
        ) : null}
      </div>
    </div>
  )
}
