import {
  BellIcon,
  BriefcaseIcon,
  CakeIcon,
  CalendarIcon,
  FlagIcon,
  GiftIcon,
  GraduationCapIcon,
  HeartIcon,
  HomeIcon,
  MusicIcon,
  PartyPopperIcon,
  PlaneIcon,
  StarIcon,
  StethoscopeIcon,
  TrophyIcon,
  UsersIcon,
  UtensilsIcon,
  type LucideIcon,
  type LucideProps,
} from 'lucide-react'
import type { TypeIcon } from '@/api/types'

/** Glyphs for event types. Keep in sync with `typeIcons` in internal/domain/eventtypes.go. */
const ICONS: Record<TypeIcon, LucideIcon> = {
  calendar: CalendarIcon,
  cake: CakeIcon,
  gift: GiftIcon,
  'party-popper': PartyPopperIcon,
  flag: FlagIcon,
  plane: PlaneIcon,
  heart: HeartIcon,
  star: StarIcon,
  briefcase: BriefcaseIcon,
  'graduation-cap': GraduationCapIcon,
  utensils: UtensilsIcon,
  music: MusicIcon,
  trophy: TrophyIcon,
  stethoscope: StethoscopeIcon,
  home: HomeIcon,
  users: UsersIcon,
  bell: BellIcon,
}

/** An event type's icon. Unknown names (from a newer server) fall back to a calendar. */
export function TypeGlyph({ name, ...props }: { name: string } & Omit<LucideProps, 'ref'>) {
  const Icon = ICONS[name as TypeIcon] ?? CalendarIcon
  return <Icon {...props} />
}
