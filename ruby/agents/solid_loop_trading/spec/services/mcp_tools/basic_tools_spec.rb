require "rails_helper"

RSpec.describe "McpTools Basic Tools" do
  let(:session) { TradingSession.create!(uuid: SecureRandom.uuid).tap { |s| s.add_asset("USDT", 1000, category: "deposit") } }

  describe McpTools::CancelOrder do
    it "returns error for nonexistent order" do
      result = described_class.new(session, { "order_id" => "fake" }).call
      expect(result).to include("Error: Order not found")
    end

    it "refunds USDT when cancelling buy order" do
      session.place_limit_order(symbol: "BTCUSD", side: "buy", amount: 0.1, price: 5000)
      order_id = session.trading_orders.last.order_id
      expect(session.reload.get_balance("USDT")).to eq(1000 - 500)
      
      described_class.new(session, { "order_id" => order_id }).call
      expect(session.reload.get_balance("USDT")).to eq(1000.0)
    end
  end

  describe McpTools::SetTime do
    it "moves virtual time and updates last_sync_at" do
      target_time = Time.parse("2026-05-01 10:00:00 UTC")
      result = described_class.new(session, { "timestamp" => target_time.to_i }).call
      
      expect(result).to include("Success")
      expect(session.reload.virtual_now.to_i).to eq(target_time.to_i)
    end

    it "prevents moving simulation time backwards" do
      session.set_virtual_time(Time.parse("2026-05-01 10:00:00 UTC"))
      past_time = Time.parse("2026-05-01 09:00:00 UTC")
      
      result = described_class.new(session, { "timestamp" => past_time.to_i }).call
      expect(result).to include("Error: Cannot move time backwards")
    end
  end

  describe McpTools::AddAsset do
    it "fails if amount is negative" do
      result = described_class.new(session, { "symbol" => "USDT", "amount" => -100 }).call
      expect(result).to include("Error: amount must be positive")
    end
  end
end
