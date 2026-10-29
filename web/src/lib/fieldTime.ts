import { fromWall, type FormatOptions } from './time'

/**
 * A time-of-day field value for a viewer. The stored `HH:MM` is a wall time in the event's own zone on `date`;
 * it is shown in the viewer's zone and clock style, so only the time (not a changed date) is shown.
 */
export function viewerTime(hhmm: string, o: { date: string; eventTz: string; fmt: FormatOptions }): string {
  const t = /^(\d{2}):(\d{2})$/.exec(hhmm)
  const d = /^(\d{4})-(\d{2})-(\d{2})/.exec(o.date)
  if (!t || !d) return hhmm
  const ms = fromWall({ year: Number(d[1]), month: Number(d[2]), day: Number(d[3]), hour: Number(t[1]), minute: Number(t[2]), second: 0 }, o.eventTz)
  return new Intl.DateTimeFormat(o.fmt.locale, {
    hour: 'numeric',
    minute: '2-digit',
    hourCycle: o.fmt.clock24h ? 'h23' : 'h12',
    timeZone: o.fmt.timeZone,
  }).format(ms)
}
