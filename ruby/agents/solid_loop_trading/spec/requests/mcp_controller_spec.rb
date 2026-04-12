require "rails_helper"
require "csv"

RSpec.describe "McpController", type: :request do
  let!(:candle) { OhlcCandle.create!(symbol: "BTCUSD", timestamp: Time.parse("2026-03-01 12:00:00 UTC"), open: 60000, high: 61000, low: 59000, close: 60500, volume: 10) }

  describe "POST /mcp" do
    it "initializes a session and returns a session ID" do
      post "/mcp", params: { jsonrpc: "2.0", id: 1, method: "initialize" }.to_json, headers: { "Content-Type" => "application/json" }
      
      expect(response).to have_http_status(:success)
      json = JSON.parse(response.body)
      expect(json["result"]["serverInfo"]["name"]).to eq("Trading-Demo-Server")
      expect(response.headers["Mcp-Session-Id"]).to be_present
    end

    context "with an active session" do
      let(:session) { TradingSession.create!(uuid: SecureRandom.uuid) }
      let(:headers) { { "Content-Type" => "application/json", "Mcp-Session-Id" => session.uuid } }

      it "sets simulation time" do
        target_ts = Time.parse("2026-03-01 12:30:00 UTC").to_i
        post "/mcp", params: { 
          jsonrpc: "2.0", id: 2, method: "tools/call", 
          params: { name: "set_time", arguments: { timestamp: target_ts } } 
        }.to_json, headers: headers

        expect(response).to have_http_status(:success)
        expect(session.reload.virtual_now.to_i).to eq(target_ts)
      end

      it "returns candles summary and saves CSV file" do
        OhlcCandle.delete_all
        base_time = Time.parse("2026-03-01 12:00:00 UTC")
        40.times do |i|
          OhlcCandle.create!(
            symbol: "BTCUSD",
            timestamp: base_time + i.minutes,
            open: 100 + i, high: 110 + i, low: 90 + i, close: 105 + i, volume: 100
          )
        end

        session.set_virtual_time(base_time + 1.day)
        start_ts = base_time.to_i
        
        post "/mcp", params: { 
          jsonrpc: "2.0", id: 5, method: "tools/call", 
          params: { name: "get_candles", arguments: { pair: "BTCUSD", interval: 1, start_time: start_ts } }
        }.to_json, headers: headers

        expect(response).to have_http_status(:success)
        json = JSON.parse(response.body)
        text = json["result"]["content"][0]["text"]

        expect(text).to include("First 10:")
        expect(text).to include("Sampled (10%..90%):")
        expect(text).to include("Last 10:")
        expect(text).to include("Total: 40 candles")
        expect(text).to include("CSV sample (first 2 lines):")
        expect(text).to include("Timestamp,Open,High,Low,Close,Volume")

        csv_filename = "BTCUSD-#{start_ts}-#{session.virtual_now.to_i}.csv"
        csv_path = Rails.root.join("storage", "external", "data", session.uuid, csv_filename)
        expect(File.exist?(csv_path)).to be_truthy
      end

      it "performs full trading cycle" do
        OhlcCandle.delete_all
        base_time = Time.parse("2026-03-01 12:00:00 UTC")
        # Market price is 50000
        OhlcCandle.create!(symbol: "BTCUSD", timestamp: base_time, open: 50000, high: 50000, low: 50000, close: 50000, volume: 1)
        
        session.set_virtual_time(base_time + 1.minute)

        # 1. Add 100,000 USDT
        post "/mcp", params: { 
          jsonrpc: "2.0", id: 6, method: "tools/call", 
          params: { name: "add_asset", arguments: { symbol: "USDT", amount: 100000 } } 
        }.to_json, headers: headers
        expect(session.reload.get_balance("USDT")).to eq(100000.0)

        # 2. Buy 1 BTC
        # Execution price = 50000 * 1.001 = 50050. Cost = 50050
        post "/mcp", params: {
          jsonrpc: "2.0", id: 7, method: "tools/call",
          params: { name: "add_order", arguments: { pair: "BTCUSD", type: "buy", ordertype: "market", volume: 1 } }
        }.to_json, headers: headers
        
        expect(session.reload.get_balance("BTCUSD")).to eq(1.0)
        expect(session.get_balance("USDT")).to be_within(0.001).of(49950.0)

        # 3. Sell 0.5 BTC
        # Execution price = 50000 * 0.999 = 49950. Revenue = 0.5 * 49950 = 24975
        post "/mcp", params: {
          jsonrpc: "2.0", id: 8, method: "tools/call",
          params: { name: "add_order", arguments: { pair: "BTCUSD", type: "sell", ordertype: "market", volume: 0.5 } }
        }.to_json, headers: headers
        
        expect(session.reload.get_balance("BTCUSD")).to eq(0.5)
        expect(session.get_balance("USDT")).to be_within(0.001).of(74925.0)

        # 4. Finish session
        post "/mcp", params: { 
          jsonrpc: "2.0", id: 9, method: "tools/call", 
          params: { name: "finish_trade" } 
        }.to_json, headers: headers
        
        json = JSON.parse(response.body)
        # Remaining 0.5 BTC sold at 49950 = 24975
        # Total final value = 74925 + 24975 = 99900
        expect(json["result"]["content"][0]["text"]).to include("Final Equity: 99900.0 USDT")
      end
    end
  end
end
