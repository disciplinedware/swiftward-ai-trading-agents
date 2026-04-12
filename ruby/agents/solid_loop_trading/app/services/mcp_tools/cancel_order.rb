module McpTools
  class CancelOrder < BaseTool
    SCHEMA = {
      name: "cancel_order",
      description: "Cancel an active limit order and refund reserved assets.",
      inputSchema: {
        type: "object",
        properties: {
          order_id: { type: "string", description: "ID of the order to cancel" },
          reasoning: { type: "string", description: "Why you are cancelling this order (e.g. 'price moved away from limit level', 'adjusting position size', 'market conditions changed')" }
        },
        required: ["order_id", "reasoning"]
      }
    }.freeze

    def call
      session.sync_state!
      res = session.cancel_order(arguments["order_id"], reasoning: arguments["reasoning"])
      res[:success] ? "Order cancelled." : "Error: #{res[:error]}"
    end
  end
end
