require "rails_helper"

RSpec.describe McpTools::GetOrderHistory do
  let(:session) { TradingSession.create!(uuid: SecureRandom.uuid).tap { |s| s.add_asset("USDT", 1000, category: "deposit") } }
  
  describe "#call" do
    it "returns history of executed orders with before/after snapshots" do
      # 1. Setup market data
      OhlcCandle.create!(symbol: "BTCUSD", timestamp: session.virtual_now, close: 1000)
      
      # 2. Execute a trade
      session.execute_market_trade(symbol: "BTCUSD", side: "buy", amount: 0.1)
      
      # 3. Call history tool
      result = described_class.new(session, { "limit" => 5 }).call
      
      expect(result).to include("Execution Log")
      expect(result).to include("BTCUSD")
      expect(result).to include("BUY")
      expect(result).to include("equity_before")
      expect(result).to include("equity_after")
      expect(result).to include("portfolio_before")
      expect(result).to include("portfolio_after")
      
      # Check values
      json_start = result.index("[")
      history = JSON.parse(result[json_start..-1])
      expect(history.first["amount"]).to eq(0.1)
      expect(history.first["equity_before"]).to eq(1000.0)
      expect(history.first["equity_after"]).to be < 1000.0 # Due to spread
    end

    it "returns a message if no history exists" do
      result = described_class.new(session, {}).call
      expect(result).to include("No orders recorded yet")
    end
  end
end
