module McpTools
  class GetOpenOrders < BaseTool
    SCHEMA = {
      name: "get_open_orders",
      description: "Return a list of all active limit orders that haven't been filled yet. Each order includes ID, symbol, side, amount, and limit price.",
      inputSchema: { type: "object", properties: {} }
    }.freeze

    def call
      session.sync_state!
      orders = session.open_orders || []
      if orders.empty?
        "You have no open orders."
      else
        "Open Orders:\n#{JSON.pretty_generate(orders)}"
      end
    end
  end
end
