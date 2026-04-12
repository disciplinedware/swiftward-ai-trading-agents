# Presenter for remote trading sessions identified by SolidLoop loop ID.
# Data comes from Swiftward MCP tools (trade/get_portfolio, trade/get_history, etc.).
class RemoteTradingSessionPresenter < TradingSessionPresenter
  def initialize(loop_id)
    @loop_id = loop_id
  end

  def live? = true
  def uuid = loop_record.subject&.agent_id || @loop_id.to_s

  def total_equity_usdt
    @total_equity_usdt ||= portfolio_data.dig("portfolio", "value").to_f
  end

  def total_equity_usdt_projected(offset_seconds:)
    total_equity_usdt  # Live trading — no price projection available
  end

  def equity_snapshot
    @equity_snapshot ||= begin
      data = portfolio_data
      snap = {}
      cash = data.dig("portfolio", "cash").to_f
      snap["USD"] = { amount: cash, value_usdt: cash } if cash > 0
      data.fetch("positions", []).each do |pos|
        snap[pos["pair"]] = {
          amount:     pos["qty"].to_f,
          value_usdt: pos["value"].to_f
        }
      end
      snap
    end
  end

  def virtual_now = Time.current

  def open_orders = []

  def initial_balance
    @initial_balance ||= begin
      first_fill = history_data.fetch("trades", []).select { |t| t["status"] == "fill" }.last
      if first_fill
        first_fill.dig("portfolio", "value_after").to_f + first_fill.dig("fill", "fee_value").to_f
      else
        limits_data.dig("portfolio", "day_start_value").to_f
      end
    end
  end

  def chart_points
    @chart_points ||= begin
      points = [ { val: initial_balance, time: "Start" } ]
      equity_curve.each do |point|
        val  = point.dig("portfolio", "value").to_f
        time = Time.parse(point["timestamp"]).strftime("%H:%M") rescue "?"
        points << { val: val.round(2), time: time }
      end
      points << { val: total_equity_usdt.round(2), time: "Now" }
      points
    end
  end

  def ledger_rows
    @ledger_rows ||= build_ledger_rows
  end

  def pnl
    total_equity_usdt - initial_balance
  end

  def trades_count
    history_data.fetch("trades", []).count { |t| t["status"] == "fill" }
  end

  private

  def loop_record
    @loop_record ||= SolidLoop::Loop.find(@loop_id)
  end

  def mcp_session
    @mcp_session ||= SolidLoop::McpSession.find_by!(loop_id: @loop_id, mcp_name: "trading")
  end

  def call_tool(name, arguments = {})
    mcp_tool = mcp_session.mcp_tools.find_or_create_by!(name: name)
    agent    = loop_record.agent
    result   = SolidLoop::McpToolExecutionService.call(
      mcp_tool:  mcp_tool,
      agent:     agent,
      arguments: arguments
    )
    result[:success] ? result[:result] : raise(SolidLoop::McpError, result[:result])
  end

  def portfolio_data
    @portfolio_data ||= JSON.parse(call_tool("trade/get_portfolio"))
  end

  def limits_data
    @limits_data ||= JSON.parse(call_tool("trade/get_limits"))
  end

  def history_data
    @history_data ||= JSON.parse(call_tool("trade/get_history", "limit" => 200))
  end

  def equity_curve
    @equity_curve ||= JSON.parse(
      call_tool("trade/get_portfolio_history", "limit" => 200)
    ).fetch("equity_curve", [])
  end

  def build_ledger_rows
    fills = history_data.fetch("trades", [])
                        .select { |t| t["status"] == "fill" }
                        .reverse  # API is newest-first; we need chronological

    return [] if fills.empty?

    prev_equity = nil

    fills.map.with_index do |trade, idx|
      equity_after  = trade.dig("portfolio", "value_after").to_f
      fee_value     = trade.dig("fill", "fee_value").to_f
      equity_before = equity_after + fee_value

      wait_pnl = prev_equity ? (equity_before - prev_equity) : nil

      wait_duration = if idx > 0
        cur  = Time.parse(trade["timestamp"]) rescue nil
        prev_t = Time.parse(fills[idx - 1]["timestamp"]) rescue nil
        cur && prev_t ? cur - prev_t : nil
      end

      prev_equity = equity_after

      TradingSessionPresenter::LedgerRow.new(
        executed_at:     Time.parse(trade["timestamp"]),
        side:            trade["side"],
        amount:          trade.dig("fill", "qty").to_f,
        symbol:          trade["pair"],
        price:           trade.dig("fill", "price").to_f,
        status:          trade["status"],
        reasoning:       trade.dig("metadata", "reasoning"),
        balances_before: {},
        equity_before:   equity_before,
        balances_after:  {},
        equity_after:    equity_after,
        impact:          -fee_value,
        total_pnl:       equity_after - initial_balance,
        wait_pnl:        wait_pnl,
        wait_duration:   wait_duration
      )
    end
  end
end
