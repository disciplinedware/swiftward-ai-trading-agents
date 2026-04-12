module McpTools
  class AddAsset < BaseTool
    SCHEMA = {
      name: "add_asset",
      description: "Admin only: Add assets to the session portfolio.",
      inputSchema: {
        type: "object",
        properties: {
          symbol: { type: "string" },
          amount: { type: "number" }
        },
        required: ["symbol", "amount"]
      }
    }.freeze

    def call
      symbol = arguments["symbol"]
      amount = arguments["amount"].to_f

      return "Error: symbol is required" if symbol.blank?
      return "Error: amount must be positive" if amount <= 0

      session.add_asset(symbol, amount, category: "deposit")
      "Success: Added #{amount} #{symbol}."
    end
  end
end
