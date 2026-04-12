import { NavLink } from 'react-router-dom'
import { clsx } from 'clsx'
import {
  LayoutDashboard,
  BarChart3,
  Shield,
  Fingerprint,
  ToggleLeft,
  Bot,
} from 'lucide-react'

const navItems = [
  { to: '/', label: 'Overview', icon: LayoutDashboard, end: true },
  { to: '/market', label: 'Market', icon: BarChart3 },
  { to: '/trust', label: 'Trust', icon: Fingerprint },
  { to: '/policy', label: 'Policy', icon: Shield },
  { to: '/demo', label: 'With/Without', icon: ToggleLeft },
  { to: '/claude-agent', label: 'Claude Agent', icon: Bot },
]

export function Navigation() {
  return (
    <nav className="flex items-center border-b border-surface-border bg-surface-card px-4 md:px-6 overflow-x-auto">
      <div className="mr-6 md:mr-8 flex shrink-0 items-center gap-2 py-3">
        <div className="h-7 w-7 rounded bg-accent flex items-center justify-center text-white text-xs font-bold">
          T
        </div>
        <span className="text-text-primary font-semibold text-sm">
          Trading Platform
        </span>
      </div>

      <div className="flex gap-1">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.end}
            className={({ isActive }) =>
              clsx(
                'flex shrink-0 items-center gap-2 rounded-md px-3 py-2 text-sm whitespace-nowrap transition-colors',
                isActive
                  ? 'bg-accent/15 text-accent'
                  : 'text-text-secondary hover:bg-surface-hover hover:text-text-primary'
              )
            }
          >
            <item.icon size={16} />
            {item.label}
          </NavLink>
        ))}
      </div>
    </nav>
  )
}
