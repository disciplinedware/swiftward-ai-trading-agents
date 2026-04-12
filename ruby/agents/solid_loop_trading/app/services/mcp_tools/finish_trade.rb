module McpTools
  class FinishTrade < BaseTool
    SCHEMA = {
      name: "finish_trade",
      description: "Liquidate your entire portfolio: cancels all open orders and sells all positions at current market prices. Call this when you are done trading.",
      inputSchema: {
        type: "object",
        properties: {
          reasoning: { type: "string", description: "Why you are done trading — what goal was reached or why you are stopping (e.g. 'target profit achieved', 'time horizon elapsed', 'no more suitable setups found')" }
        },
        required: ["reasoning"]
      }
    }.freeze

    def call
      session.sync_state!
      finish_reasoning = arguments["reasoning"]

      # 1. Cancel all open orders first
      (session.open_orders || []).dup.each do |order|
        session.cancel_order(order["id"], reasoning: "Finish Trade: #{finish_reasoning}")
      end

      # 2. Liquidate everything by executing market sells
      portfolio = session.portfolio

      portfolio.each do |symbol, amount|
        next if symbol == "USDT" || amount.to_f <= 0
        session.execute_market_trade(symbol: symbol, side: "sell", amount: amount.to_f, reasoning: "Finish Trade: #{finish_reasoning}")
      end

      final_equity = session.get_balance("USDT")
      "Success: Portfolio liquidated. Final Equity: #{final_equity.round(2)} USDT. All positions closed and converted to USDT."
    end
  end
end
