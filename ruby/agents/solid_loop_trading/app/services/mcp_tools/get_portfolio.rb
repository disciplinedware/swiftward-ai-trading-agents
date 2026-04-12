module McpTools
  class GetPortfolio < BaseTool
    SCHEMA = {
      name: "get_portfolio",
      description: "Get detailed status of your trading portfolio. Returns current balances, reserved funds (in open orders), market prices, and total equity value in USDT. Call this before making any trading decisions.",
      inputSchema: { type: "object", properties: {} }
    }.freeze

    def call
      session.sync_state!
      
      portfolio = session.portfolio || {}
      symbols = (portfolio.keys + (session.open_orders || []).map { |o| o["symbol"] }).uniq

      summary = {
        assets: {},
        total_equity_usdt: 0.0,
        timestamp: session.virtual_now.to_i,
        current_time: session.virtual_now.utc.to_s
      }

      symbols.each do |symbol|
        amount = session.get_balance(symbol)
        reserved = session.get_reserved_balance(symbol)
        
        next if amount <= 0 && reserved <= 0 && symbol != "USDT"

        price = (symbol == "USDT") ? 1.0 : session.market_price(symbol)
        value = price ? (amount + reserved) * price : 0.0
        
        summary[:assets][symbol] = {
          available: amount,
          reserved: reserved,
          total: (amount + reserved).round(8),
          price: price || "Price not found",
          value_usdt: value.round(2)
        }
        summary[:total_equity_usdt] += value
      end

      summary[:total_equity_usdt] = summary[:total_equity_usdt].round(2)
      
      "Current Portfolio Status:\n#{JSON.pretty_generate(summary)}"
    end
  end
end
