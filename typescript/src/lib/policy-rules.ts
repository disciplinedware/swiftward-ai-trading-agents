// Static policy rule definitions - derived from rules.yaml v1 and v2.
// These are displayed in the Policy screen. Since rules are baked into
// the Swiftward policy engine (not queryable via API), we define them here.
//
// Last synced: 2026-04-02 from swiftward/policies/rulesets/trading/

export interface PolicyRule {
  id: number
  name: string
  ruleKey: string
  eventType: 'trade_order' | 'heartbeat' | 'execution_report'
  mode: 'sync' | 'async'
  threshold: string
  description: string
  tag?: string
  priority: number
  // Minimal YAML snippet for display (not the full rule)
  yamlSnippet: string
}

export interface ShadowDiff {
  rule: string
  v1Value: string
  v2Value: string
}

export const V1_RULES: PolicyRule[] = [
  {
    id: 0,
    name: 'Halted Agent Check',
    ruleKey: 'halted_check',
    eventType: 'trade_order',
    mode: 'sync',
    priority: 110,
    threshold: 'halted = true',
    description: 'Reject if agent has been halted by heartbeat or risk manager',
    tag: 'halted',
    yamlSnippet: `halted_check:
  enabled: true
  mode: sync
  all:
    - path: "event.type"
      op: eq
      value: "trade_order"
    - path: "state.agent.labels.halted"
      op: eq
      value: true
  effects:
    verdict: rejected
    priority: 110
    response:
      reason: "Agent is halted - trading paused by risk manager or heartbeat"
      tag: "halted"`,
  },
  {
    id: 1,
    name: 'Close-Only Mode',
    ruleKey: 'close_only_mode',
    eventType: 'trade_order',
    mode: 'sync',
    priority: 105,
    threshold: 'tier 3 (drawdown > 5%)',
    description: 'Intraday drawdown hit tier 3 - only risk-reducing exits allowed, new positions blocked',
    tag: 'close_only',
    yamlSnippet: `close_only_mode:
  enabled: true
  mode: sync
  all:
    - path: "event.type"
      op: eq
      value: "trade_order"
    - path: "signals.risk_tier"
      op: gte
      value: 3
    - path: "signals.is_risk_reducing"
      op: eq
      value: false
  effects:
    verdict: rejected
    priority: 105
    response:
      reason: "Close-only mode active (intraday drawdown > 5%) - only position exits allowed"
      tag: "close_only"`,
  },
  {
    id: 2,
    name: 'Loss Streak Pause',
    ruleKey: 'loss_streak_pause',
    eventType: 'trade_order',
    mode: 'sync',
    priority: 100,
    threshold: '>= 3 consecutive losses',
    description: '3 consecutive losing closes blocks new positions (exits still allowed)',
    tag: 'loss_streak',
    yamlSnippet: `loss_streak_pause:
  enabled: true
  mode: sync
  all:
    - path: "event.type"
      op: eq
      value: "trade_order"
    - path: "event.data.portfolio.consecutive_losses"
      op: gte
      value: "{{ constants.max_consecutive_losses }}"  # 3
    - path: "signals.is_risk_reducing"
      op: eq
      value: false
  effects:
    verdict: rejected
    priority: 100
    response:
      reason: "3 consecutive losing trades - new positions paused (exits still allowed)"
      tag: "loss_streak"`,
  },
  {
    id: 3,
    name: 'Tier 2 Order Size',
    ruleKey: 'tier2_order_size',
    eventType: 'trade_order',
    mode: 'sync',
    priority: 95,
    threshold: '> 5% of portfolio (tier 2)',
    description: 'Warning tier: reduced position limit when intraday drawdown > 3.5%',
    tag: 'risk_limit',
    yamlSnippet: `tier2_order_size:
  enabled: true
  mode: sync
  all:
    - path: "event.type"
      op: eq
      value: "trade_order"
    - path: "signals.risk_tier"
      op: gte
      value: 2
    - path: "signals.position_risk"
      op: gte
      value: "{{ constants.max_order_pct_tier2 }}"  # 0.05
  effects:
    verdict: rejected
    priority: 95
    response:
      reason: "Warning tier: order exceeds 5% of portfolio"
      tag: "risk_limit"`,
  },
  {
    id: 4,
    name: 'Tier 1 Order Size',
    ruleKey: 'tier1_order_size',
    eventType: 'trade_order',
    mode: 'sync',
    priority: 93,
    threshold: '> 10% of portfolio (tier 1)',
    description: 'Caution tier: reduced position limit when intraday drawdown > 2%',
    tag: 'risk_limit',
    yamlSnippet: `tier1_order_size:
  enabled: true
  mode: sync
  all:
    - path: "event.type"
      op: eq
      value: "trade_order"
    - path: "signals.risk_tier"
      op: gte
      value: 1
    - path: "signals.position_risk"
      op: gte
      value: "{{ constants.max_order_pct_tier1 }}"  # 0.10
  effects:
    verdict: rejected
    priority: 93
    response:
      reason: "Caution tier: order exceeds 10% of portfolio"
      tag: "risk_limit"`,
  },
  {
    id: 5,
    name: 'Max Order Size',
    ruleKey: 'max_order_size',
    eventType: 'trade_order',
    mode: 'sync',
    priority: 90,
    threshold: '> 15% of portfolio',
    description: 'Tier 0 (normal): single order too large relative to portfolio value',
    tag: 'risk_limit',
    yamlSnippet: `max_order_size:
  enabled: true
  mode: sync
  all:
    - path: "event.type"
      op: eq
      value: "trade_order"
    - path: "signals.position_risk"
      op: gte
      value: "{{ constants.max_order_pct }}"  # 0.15
  effects:
    verdict: rejected
    priority: 90
    response:
      reason: "Order exceeds 15% of portfolio"
      tag: "risk_limit"`,
  },
  {
    id: 6,
    name: 'Pair Whitelist',
    ruleKey: 'pair_whitelist',
    eventType: 'trade_order',
    mode: 'sync',
    priority: 88,
    threshold: 'whitelist: 45 pairs (15 assets x USD/USDC/USDT)',
    description: 'Reject trades on pairs not in the permitted list',
    tag: 'pair_restriction',
    yamlSnippet: `pair_whitelist:
  enabled: true
  mode: sync
  all:
    - path: "event.type"
      op: eq
      value: "trade_order"
    - path: "event.data.order.pair"
      op: not_in
      value: "{{ constants.permitted_pairs }}"
  effects:
    verdict: rejected
    priority: 88
    response:
      reason: "Pair not in permitted list"
      tag: "pair_restriction"`,
  },
  {
    id: 7,
    name: 'Tier 2 Concentration',
    ruleKey: 'tier2_concentration',
    eventType: 'trade_order',
    mode: 'sync',
    priority: 87,
    threshold: '> 25% single asset (tier 2)',
    description: 'Warning tier: reduced concentration limit when intraday drawdown > 3.5%',
    tag: 'concentration',
    yamlSnippet: `tier2_concentration:
  enabled: true
  mode: sync
  all:
    - path: "event.type"
      op: eq
      value: "trade_order"
    - path: "signals.risk_tier"
      op: gte
      value: 2
    - path: "signals.concentration_check"
      op: gte
      value: "{{ constants.max_concentration_tier2 }}"  # 0.25
  effects:
    verdict: rejected
    priority: 87
    response:
      reason: "Warning tier: concentration exceeds 25%"
      tag: "concentration"`,
  },
  {
    id: 8,
    name: 'Tier 1 Concentration',
    ruleKey: 'tier1_concentration',
    eventType: 'trade_order',
    mode: 'sync',
    priority: 86,
    threshold: '> 35% single asset (tier 1)',
    description: 'Caution tier: reduced concentration limit when intraday drawdown > 2%',
    tag: 'concentration',
    yamlSnippet: `tier1_concentration:
  enabled: true
  mode: sync
  all:
    - path: "event.type"
      op: eq
      value: "trade_order"
    - path: "signals.risk_tier"
      op: gte
      value: 1
    - path: "signals.concentration_check"
      op: gte
      value: "{{ constants.max_concentration_tier1 }}"  # 0.35
  effects:
    verdict: rejected
    priority: 86
    response:
      reason: "Caution tier: concentration exceeds 35%"
      tag: "concentration"`,
  },
  {
    id: 9,
    name: 'Concentration Limit',
    ruleKey: 'concentration_limit',
    eventType: 'trade_order',
    mode: 'sync',
    priority: 85,
    threshold: '> 50% single asset',
    description: 'Tier 0 (normal): too much exposure concentrated in one asset',
    tag: 'concentration',
    yamlSnippet: `concentration_limit:
  enabled: true
  mode: sync
  all:
    - path: "event.type"
      op: eq
      value: "trade_order"
    - path: "signals.concentration_check"
      op: gte
      value: "{{ constants.max_concentration }}"  # 0.50
  effects:
    verdict: rejected
    priority: 85
    response:
      reason: "Concentration exceeds 50%"
      tag: "concentration"`,
  },
  {
    id: 10,
    name: 'Order Rate Limit',
    ruleKey: 'order_rate_limit',
    eventType: 'trade_order',
    mode: 'sync',
    priority: 80,
    threshold: '> 50 orders/10min',
    description: 'Too many orders in a 10-minute window',
    tag: 'velocity',
    yamlSnippet: `order_rate_limit:
  enabled: true
  mode: sync
  all:
    - path: "event.type"
      op: eq
      value: "trade_order"
    - path: "signals.order_rate"
      op: gte
      value: "{{ constants.max_orders_10m }}"  # 50
  effects:
    verdict: rejected
    priority: 80
    response:
      reason: "Order rate exceeded 50 orders/10min"
      tag: "velocity"`,
  },
  {
    id: 11,
    name: 'Order Counter',
    ruleKey: 'count_orders',
    eventType: 'trade_order',
    mode: 'sync',
    priority: 1,
    threshold: 'always fires',
    description: 'Increments rate bucket for every trade order (never rejects)',
    yamlSnippet: `count_orders:
  enabled: true
  mode: sync
  all:
    - path: "event.type"
      op: eq
      value: "trade_order"
  effects:
    verdict: approved
    priority: 1
    state_changes:
      agent:
        change_buckets:
          orders_10m: 1`,
  },
  {
    id: 12,
    name: 'Trailing 24h Drawdown Halt',
    ruleKey: 'heartbeat_trailing_24h',
    eventType: 'heartbeat',
    mode: 'sync',
    priority: 96,
    threshold: 'peak loss > 8% (24h rolling)',
    description: 'Halts agent when trailing 24h drawdown from rolling peak exceeds 8%',
    tag: 'heartbeat_drawdown',
    yamlSnippet: `heartbeat_trailing_24h:
  enabled: true
  mode: sync
  all:
    - path: "event.type"
      op: eq
      value: "heartbeat"
    - path: "signals.drawdown_heartbeat.peak_drawdown_pct"
      op: lte
      value: "{{ constants.max_drawdown_trailing_24h }}"  # -0.08
  effects:
    verdict: rejected
    priority: 96
    state_changes:
      agent:
        set_labels:
          halted:
    response:
      reason: "Trailing 24h drawdown exceeded 8% - agent halted"
      tag: "heartbeat_drawdown"`,
  },
  {
    id: 13,
    name: 'Absolute Floor Halt',
    ruleKey: 'heartbeat_absolute',
    eventType: 'heartbeat',
    mode: 'sync',
    priority: 97,
    threshold: 'total loss > 15% from initial',
    description: 'Kill switch - halts agent when total loss from initial capital exceeds 15%',
    tag: 'heartbeat_drawdown',
    yamlSnippet: `heartbeat_absolute:
  enabled: true
  mode: sync
  all:
    - path: "event.type"
      op: eq
      value: "heartbeat"
    - path: "signals.drawdown_heartbeat.floor_drawdown_pct"
      op: lte
      value: "{{ constants.max_drawdown_absolute }}"  # -0.15
  effects:
    verdict: rejected
    priority: 97
    state_changes:
      agent:
        set_labels:
          halted:
    response:
      reason: "Absolute drawdown exceeded 15% floor - agent halted"
      tag: "heartbeat_drawdown"`,
  },
  {
    id: 14,
    name: 'Track Fill',
    ruleKey: 'track_fill',
    eventType: 'execution_report',
    mode: 'async',
    priority: 0,
    threshold: 'status == "fill"',
    description: 'Increments fill counter on successful execution',
    yamlSnippet: `track_fill:
  enabled: true
  mode: async
  all:
    - path: "event.type"
      op: eq
      value: "execution_report"
    - path: "event.data.status"
      op: eq
      value: "fill"
  effects:
    verdict: approved
    state_changes:
      agent:
        change_counters:
          fill_count: 1`,
  },
  {
    id: 15,
    name: 'Router Rejection Alert',
    ruleKey: 'router_rejection_alert',
    eventType: 'execution_report',
    mode: 'async',
    priority: 0,
    threshold: 'status == "reject"',
    description: 'Alerts when Risk Router rejects a Swiftward-approved trade - signals policy gap',
    yamlSnippet: `router_rejection_alert:
  enabled: true
  mode: async
  all:
    - path: "event.type"
      op: eq
      value: "execution_report"
    - path: "event.data.status"
      op: eq
      value: "reject"
  effects:
    verdict: flagged
    state_changes:
      agent:
        change_counters:
          reject_count: 1`,
  },
]

// v1 vs v2 threshold comparison for shadow policy panel.
// v1 uses graduated tiers (0-3 based on intraday drawdown).
// v2 uses flat thresholds with circuit breaker cooldown.
export const SHADOW_DIFFS: ShadowDiff[] = [
  { rule: 'Position Limit', v1Value: '15% / 10% / 5% (tiered)', v2Value: '10% (flat)' },
  { rule: 'Concentration', v1Value: '50% / 35% / 25% (tiered)', v2Value: '35% (flat)' },
  { rule: 'Order Rate', v1Value: '50/10min', v2Value: '25/10min' },
  { rule: 'Intraday Drawdown', v1Value: '5% (close-only mode)', v2Value: '3% (circuit breaker)' },
  { rule: 'Trailing 24h Halt', v1Value: '8%', v2Value: '7%' },
  { rule: 'Absolute Floor Halt', v1Value: '15%', v2Value: '15%' },
  { rule: 'Loss Streak Pause', v1Value: '3 consecutive', v2Value: '-' },
  { rule: 'Leverage Cap (v2 only)', v1Value: '-', v2Value: '2x max' },
  { rule: 'Tiered Risk Limits', v1Value: '4 tiers (0-3)', v2Value: 'flat (no tiers)' },
]
