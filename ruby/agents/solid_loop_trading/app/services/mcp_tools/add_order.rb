module McpTools
  class AddOrder < BaseTool
    SCHEMA = {
      name: "add_order",
      description: "Place a buy or sell order. Market orders execute immediately at the current price (0.1% taker fee). Limit orders rest in the book until the target price is reached (lower fees — add oflags: 'post' to guarantee maker status).",
      inputSchema: {
        type: "object",
        properties: {
          pair: { type: "string", description: "Asset pair (e.g. BTCUSD)" },
          type: { type: "string", enum: ["buy", "sell"] },
          ordertype: { type: "string", enum: ["market", "limit"] },
          volume: { type: "number" },
          price: { type: "number" },
          oflags: { type: "string" },
          reasoning: { type: "string", description: "Justify this order: what analysis, signal, or market condition led to this decision (e.g. 'RSI oversold on 1h, price bouncing off support', 'taking profit at resistance after 15% gain')" }
        },
        required: ["pair", "type", "ordertype", "volume", "reasoning"]
      }
    }.freeze

    def call
      pair = arguments["pair"]
      side = arguments["type"]
      ordertype = arguments["ordertype"]
      volume = arguments["volume"].to_f
      price = arguments["price"]&.to_f
      flags = arguments["oflags"].to_s.split(",")
      reasoning = arguments["reasoning"]

      # 1. Capture BEFORE state
      equity_before = session.total_equity_usdt
      
      # Validations...
      return "Error: 'pair' is required" if pair.blank?
      unless OhlcCandle::TICKERS.include?(pair)
        return "Error: Unknown pair '#{pair}'. Supported pairs are: #{OhlcCandle::TICKERS.join(', ')}"
      end
      return "Error: 'type' must be 'buy' or 'sell'" unless %w[buy sell].include?(side)
      return "Error: 'ordertype' must be 'market' or 'limit'" unless %w[market limit].include?(ordertype)
      return "Error: Volume must be positive" if volume <= 0
      return "Error: Price must be positive" if price && price <= 0

      res = nil
      if ordertype == "market"
        res = session.execute_market_trade(symbol: pair, side: side, amount: volume, reasoning: reasoning)
      else
        return "Error: 'price' parameter is required for limit orders." unless price
        res = session.place_limit_order(symbol: pair, side: side, amount: volume, price: price, post_only: flags.include?("post"), reasoning: reasoning)
      end

      return "Error: #{res[:error]}" unless res[:success]

      # 2. Capture AFTER state
      equity_after = session.total_equity_usdt
      impact = equity_after - equity_before
      snapshot = session.equity_snapshot

      # 3. Format detailed response
      msg = "Success: #{ordertype.capitalize} order processed.\n"
      msg += "----------------------------------------\n"
      msg += "Equity BEFORE: $#{equity_before.round(2)}\n"
      
      if ordertype == "market"
        msg += ">> #{side.upcase} #{volume} #{pair} @ $#{res[:price].round(2)} (executed immediately)\n"
      else
        msg += ">> PLACED #{side.upcase} LIMIT: #{volume} #{pair} @ $#{price.round(2)} (PENDING)\n"
      end

      msg += "Equity AFTER: $#{equity_after.round(2)} (Net Impact: $#{impact.round(2)})\n"
      msg += "----------------------------------------\n"
      msg += "Portfolio Breakdown (in USDT equivalent):\n"
      snapshot.each do |symbol, data|
        msg += "- #{symbol}: $#{data[:value_usdt]} (#{data[:amount]})\n"
      end
      
      msg
    end
  end
end
