require "rails_helper"

RSpec.describe McpTools::AddOrder do
  let(:session) { TradingSession.create!(uuid: SecureRandom.uuid).tap { |s| s.add_asset("USDT", 1000, category: "deposit") } }
  
  describe "#call" do
    it "executes market buy when funds are sufficient" do
      # Mock market price
      OhlcCandle.create!(symbol: "BTCUSD", timestamp: session.virtual_now, close: 1000)
      
      arguments = { "pair" => "BTCUSD", "type" => "buy", "ordertype" => "market", "volume" => 0.5 }
      result = described_class.new(session, arguments).call
      
      expect(result).to include("Market order processed")
      expect(result).to include("@ $1001.0") # 1000 + 0.1% spread
      expect(session.reload.get_balance("BTCUSD")).to eq(0.5)
      expect(session.get_balance("USDT")).to eq(1000 - (0.5 * 1001))
    end

    it "fails market buy when funds are insufficient" do
      OhlcCandle.create!(symbol: "BTCUSD", timestamp: session.virtual_now, close: 1000)
      
      arguments = { "pair" => "BTCUSD", "type" => "buy", "ordertype" => "market", "volume" => 2.0 }
      expect {
        described_class.new(session, arguments).call
      }.to raise_error("Insufficient USDT")
    end

    it "fails if volume is zero or negative" do
      arguments = { "pair" => "BTCUSD", "type" => "buy", "ordertype" => "market", "volume" => -1.0 }
      result = described_class.new(session, arguments).call
      expect(result).to include("Error")
    end

    it "fails if price is negative for limit order" do
      arguments = { "pair" => "BTCUSD", "type" => "buy", "ordertype" => "limit", "volume" => 0.1, "price" => -500 }
      result = described_class.new(session, arguments).call
      expect(result).to include("Error")
    end

    it "fails if pair is unknown (no candles in DB)" do
      arguments = { "pair" => "UNKNOWN", "type" => "buy", "ordertype" => "market", "volume" => 0.1 }
      result = described_class.new(session, arguments).call
      expect(result).to include("Error: Unknown pair 'UNKNOWN'")
      expect(result).to include("BTCUSD") # Should list supported pairs
    end

    it "requires price for limit orders" do
      arguments = { "pair" => "BTCUSD", "type" => "buy", "ordertype" => "limit", "volume" => 0.1 }
      result = described_class.new(session, arguments).call
      
      expect(result).to include("Error: 'price' parameter is required")
    end

    it "reserves assets for limit sell order" do
      session.add_asset("BTCUSD", 1.0)
      arguments = { "pair" => "BTCUSD", "type" => "sell", "ordertype" => "limit", "volume" => 0.4, "price" => 2000 }
      result = described_class.new(session, arguments).call
      
      expect(result).to include("Limit order processed")
      expect(result).to include("PLACED SELL LIMIT")
      expect(session.reload.get_balance("BTCUSD")).to eq(0.6)
      expect(session.get_reserved_balance("BTCUSD")).to eq(0.4)
    end
  end
end
