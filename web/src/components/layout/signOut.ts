import { endpoints } from '@/api/endpoints'

export async function signOut() {
  try {
    const { logoutUrl } = await endpoints.logout()
    window.location.assign(logoutUrl || '/')
  } catch {
    window.location.assign('/')
  }
}
