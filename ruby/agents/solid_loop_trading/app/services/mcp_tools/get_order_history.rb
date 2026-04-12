module McpTools
  class GetOrderHistory < BaseTool
    SCHEMA = {
      name: "get_order_history",
      description: "Retrieve a detailed log of your executed and cancelled orders. Each entry includes the portfolio state before and after the trade, allowing you to see the real impact on your equity (slippage, fees).",
      inputSchema: {
        type: "object",
        properties: {
          limit: { type: "integer", description: "Number of recent orders to return. Default is 5.", default: 5 }
        }
      }
    }.freeze

    def call
      session.sync_state!
      
      limit = (arguments["limit"] || 5).to_i
      limit = 20 if limit > 20 # Cap it to preserve context
      
      orders = session.trading_orders.order(id: :desc).limit(limit).to_a
      return "No orders recorded yet." if orders.empty?

      # Calculate initial balance for PnL
      initial_balance = session.ledger_entries.where(category: "deposit", asset: "USDT").sum(:amount).to_f
      if initial_balance == 0
         mcp_session = SolidLoop::McpSession.find_by(session_id: session.uuid)
         initial_balance = mcp_session&.loop&.subject&.trading_scenario&.initial_balance&.to_f || 100000.0
      end

      all_ledger = session.ledger_entries.order(id: :asc).to_a

      history_log = orders.map do |order|
        # Portfolio After
        after_ledger = all_ledger.select { |l| l.trading_order_id.to_i <= order.id || (l.trading_order_id.nil? && l.created_at <= order.created_at) }
        balances_after = after_ledger.group_by(&:asset).transform_values { |entries| entries.sum(&:amount).to_f.round(8) }.reject { |_, v| v == 0 }
        equity_after = balances_after.sum { |s, a| a * ((s == "USDT" ? 1.0 : session.market_price(s, at_time: order.executed_at)) || 0.0) }.round(2)

        # Portfolio Before
        before_ledger = all_ledger.select { |l| (l.trading_order_id.nil? && l.created_at < order.created_at) || (l.trading_order_id.present? && l.trading_order_id.to_i < order.id) }
        balances_before = before_ledger.group_by(&:asset).transform_values { |entries| entries.sum(&:amount).to_f.round(8) }.reject { |_, v| v == 0 }
        equity_before = balances_before.sum { |s, a| a * ((s == "USDT" ? 1.0 : session.market_price(s, at_time: order.executed_at)) || 0.0) }.round(2)
        
        impact = (equity_after - equity_before).round(2)
        total_pnl = (equity_after - initial_balance).round(2)

        {
          time_virtual: (order.executed_at || order.created_at).utc.to_s,
          order_id: order.order_id,
          type: order.side.upcase,
          pair: order.symbol,
          amount: order.amount.to_f,
          price: order.price.to_f,
          status: order.status,
          equity_before: equity_before,
          equity_after: equity_after,
          net_impact_usdt: impact,
          total_session_pnl: total_pnl,
          portfolio_before: balances_before,
          portfolio_after: balances_after
        }
      end

      "Execution Log (Most Recent First):\n#{JSON.pretty_generate(history_log)}"
    end
  end
end
