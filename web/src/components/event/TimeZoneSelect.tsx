import { useMemo } from 'react'
import { NativeSelect } from '@/components/ui/input'
import { allTimeZones, timeZoneLabel } from '@/lib/time'

/** Native <select> of IANA zones: accessible, fast with ~400 options, type-ahead built in. */
export function TimeZoneSelect({
  id,
  value,
  onChange,
  includeBrowserDefault,
  browserDefaultLabel,
  ...rest
}: {
  id: string
  value: string
  onChange: (tz: string) => void
  includeBrowserDefault?: boolean
  browserDefaultLabel?: string
  'aria-describedby'?: string
  'aria-invalid'?: boolean
}) {
  const zones = useMemo(() => {
    const list = allTimeZones()
    return value && !list.includes(value) ? [value, ...list] : list
  }, [value])
  return (
    <NativeSelect id={id} value={value} onChange={(e) => onChange(e.target.value)} {...rest}>
      {includeBrowserDefault ? <option value="">{browserDefaultLabel}</option> : null}
      {zones.map((z) => (
        <option key={z} value={z}>
          {timeZoneLabel(z)}
        </option>
      ))}
    </NativeSelect>
  )
}
