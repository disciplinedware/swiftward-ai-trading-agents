import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { Toaster } from 'react-hot-toast'
import { Layout } from './components/Layout'
import { Overview } from './pages/Overview'
import { AgentDetail } from './pages/AgentDetail'
import { Market } from './pages/Market'
import { Evidence } from './pages/Evidence'
import { Policy } from './pages/Policy'
import { Demo } from './pages/Demo'
import { ClaudeAgent } from './pages/ClaudeAgent'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
      staleTime: 5000,
    },
  },
})

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<Overview />} />
            <Route path="/agents/:id" element={<AgentDetail />} />
            <Route path="/market" element={<Market />} />
            <Route path="/trust" element={<Evidence />} />
            <Route path="/evidence" element={<Navigate to="/trust" replace />} />
            <Route path="/policy" element={<Policy />} />
            <Route path="/demo" element={<Demo />} />
            <Route path="/claude-agent" element={<ClaudeAgent />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
        <Toaster
          position="top-right"
          toastOptions={{
            duration: 4000,
            style: {
              background: '#1a1d26',
              color: '#e1e4ea',
              border: '1px solid #2a2d36',
            },
            success: {
              duration: 3000,
              iconTheme: { primary: '#22c55e', secondary: '#fff' },
            },
            error: {
              duration: 5000,
              iconTheme: { primary: '#ef4444', secondary: '#fff' },
            },
          }}
        />
      </BrowserRouter>
    </QueryClientProvider>
  )
}

export default App
