import {
  BellIcon,
  BriefcaseIcon,
  CakeIcon,
  CalendarIcon,
  FlagIcon,
  Gamepad2Icon,
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
  createLucideIcon,
  type LucideIcon,
  type LucideProps,
} from 'lucide-react'
import type { TypeIcon } from '@/api/types'

/** An old movie camera with two film reels on top, which Lucide doesn't ship. */
const MovieCameraIcon = createLucideIcon('movie-camera', [
  ['circle', { cx: '7', cy: '6', r: '3', key: 'reel-a' }],
  ['circle', { cx: '14.5', cy: '6', r: '3', key: 'reel-b' }],
  ['rect', { x: '2', y: '10', width: '15', height: '10', rx: '2', key: 'body' }],
  ['path', { d: 'm17 13 5-2.5v9L17 17', key: 'lens' }],
])

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
  gamepad: Gamepad2Icon,
  film: MovieCameraIcon,
}

/** An event type's icon. Unknown names (from a newer server) fall back to a calendar. */
export function TypeGlyph({ name, ...props }: { name: string } & Omit<LucideProps, 'ref'>) {
  const Icon = ICONS[name as TypeIcon] ?? CalendarIcon
  return <Icon {...props} />
}
