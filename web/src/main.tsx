import '@fontsource-variable/inter'
import './index.css'
import './i18n'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { isApiError } from '@/api/client'
import { Toaster } from '@/components/ui/toast'
import { TooltipProvider } from '@/components/ui/tooltip'
import { router } from './router'
import { ThemeController } from './themes/ThemeController'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Live updates arrive over SSE, so don't refetch on every focus.
      refetchOnWindowFocus: false,
      retry: (count, err) => count < 2 && !(isApiError(err) && err.status >= 400 && err.status < 500),
    },
    mutations: { retry: false },
  },
})

const root = document.getElementById('root')
if (!root) throw new Error('#root missing')

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={300}>
        <ThemeController />
        <RouterProvider router={router} />
        <Toaster />
      </TooltipProvider>
    </QueryClientProvider>
  </StrictMode>,
)
