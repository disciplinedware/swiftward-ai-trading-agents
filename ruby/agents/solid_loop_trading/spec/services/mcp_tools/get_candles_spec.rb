require "rails_helper"

RSpec.describe McpTools::GetCandles do
  let(:session) { TradingSession.create!(uuid: SecureRandom.uuid) }
  let(:base_time) { Time.parse("2026-03-01 12:00:00 UTC") }

  before do
    session.set_virtual_time(base_time + 1.hour)
  end

  describe "#call" do
    it "fails if start_time is in the future relative to virtual now" do
      arguments = { 
        "pair" => "BTCUSD", 
        "interval" => 1, 
        "start_time" => (session.virtual_now + 1.minute).to_i 
      }
      result = described_class.new(session, arguments).call
      expect(result).to include("Error: start_time must be before end_time")
    end

    it "fails if interval is invalid (zero or negative)" do
      arguments = { "pair" => "BTCUSD", "interval" => 0, "start_time" => base_time.to_i }
      result = described_class.new(session, arguments).call
      expect(result).to include("Error")
    end

    it "fails if start_time equals end_time" do
      arguments = { "pair" => "BTCUSD", "interval" => 1, "start_time" => base_time.to_i, "end_time" => base_time.to_i }
      result = described_class.new(session, arguments).call
      expect(result).to include("Error")
    end

    it "returns error if no data found (unknown pair)" do
      arguments = { 
        "pair" => "NONEXISTENT", 
        "interval" => 60, 
        "start_time" => base_time.to_i 
      }
      result = described_class.new(session, arguments).call
      expect(result).to include("Error: Unknown pair 'NONEXISTENT'")
    end

    it "trims end_time to virtual_now if it exceeds it" do
      OhlcCandle.create!(symbol: "BTCUSD", timestamp: base_time, close: 100)
      # Create a candle in the "future"
      OhlcCandle.create!(symbol: "BTCUSD", timestamp: session.virtual_now + 10.minutes, close: 200)
      
      arguments = { 
        "pair" => "BTCUSD", 
        "interval" => 1, 
        "start_time" => base_time.to_i,
        "end_time" => (session.virtual_now + 1.hour).to_i
      }
      
      result = described_class.new(session, arguments).call
      expect(result).to include("Total: 1 candles") # Should only see the first candle
      expect(result).not_to include("O:200")
    end
  end
end
