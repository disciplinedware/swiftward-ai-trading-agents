# Abstract interface for trading session views.
# Both local (DB-backed) and remote (MCP-backed) sessions must implement this.
class TradingSessionPresenter
  LedgerRow = Struct.new(
    :executed_at, :side, :amount, :symbol, :price, :status, :reasoning,
    :balances_before, :equity_before,
    :balances_after, :equity_after,
    :impact, :total_pnl, :wait_pnl, :wait_duration,
    keyword_init: true
  )

  def live? = false
  def uuid = raise NotImplementedError
  def initial_balance = raise NotImplementedError
  def total_equity_usdt = raise NotImplementedError
  def total_equity_usdt_projected(offset_seconds:) = raise NotImplementedError
  def equity_snapshot = raise NotImplementedError
  def virtual_now = raise NotImplementedError
  def open_orders = raise NotImplementedError
  def chart_points = raise NotImplementedError
  def ledger_rows = raise NotImplementedError
end
