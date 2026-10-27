import type { TFunction } from 'i18next'
import { isApiError, UploadAbortedError } from '@/api/client'

/** A user-facing message for any thrown error. Prefers the server's problem detail. */
export function errorMessage(err: unknown, t: TFunction): string {
  if (err instanceof UploadAbortedError) return t('upload.cancelled')
  if (isApiError(err)) {
    if (err.problem.detail) return err.problem.detail
    switch (err.status) {
      case 0:
        return t('errors.network')
      case 403:
        return t('errors.forbidden')
      case 404:
        return t('errors.notFound')
      case 409:
        return t('errors.conflict')
      case 412:
        return t('errors.preconditionRequired')
      case 413:
        return t('errors.tooLarge')
      case 415:
        return t('errors.unsupportedType')
      case 422:
        return err.problem.errors?.[0]?.message ?? t('errors.validation')
      case 429:
        return err.retryAfter ? t('errors.rateLimitedRetry', { seconds: err.retryAfter }) : t('errors.rateLimited')
      default:
        return err.problem.title || t('errors.generic')
    }
  }
  return t('errors.generic')
}
