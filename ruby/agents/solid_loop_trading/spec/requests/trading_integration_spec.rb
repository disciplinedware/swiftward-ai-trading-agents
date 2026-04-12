require "rails_helper"

RSpec.describe "Trading Integration", type: :request do
  include ActiveSupport::Testing::TimeHelpers

  # Подготовка данных: создаем свечи для BTCUSD на 1 марта 2026 года
  let(:base_time) { Time.parse("2026-03-01 12:00:00 UTC") }
  
  before do
    OhlcCandle.delete_all
    NewsItem.delete_all
    # Свеча на 12:00: цена 60,000
    OhlcCandle.create!(symbol: "BTCUSD", timestamp: base_time, open: 60000, high: 60100, low: 59900, close: 60000, volume: 1)
    # Свеча на 12:01: цена падает до 59,400 (чтобы сработала лимитка на покупку)
    OhlcCandle.create!(symbol: "BTCUSD", timestamp: base_time + 1.minute, open: 60000, high: 60000, low: 59400, close: 59500, volume: 1)
    # Свеча на 13:00: цена выросла до 65,000
    OhlcCandle.create!(symbol: "BTCUSD", timestamp: base_time + 1.hour, open: 64000, high: 65100, low: 63900, close: 65000, volume: 1)
    # Свечи для ETHUSD
    OhlcCandle.create!(symbol: "ETHUSD", timestamp: base_time, open: 3000, high: 3050, low: 2950, close: 3000, volume: 1)
    OhlcCandle.create!(symbol: "ETHUSD", timestamp: base_time + 1.minute, open: 3000, high: 3000, low: 3000, close: 3000, volume: 1)
  end

  def extract_portfolio(response_body)
    text = JSON.parse(response_body)["result"]["content"][0]["text"]
    json_part = text.split("\n", 2).last
    JSON.parse(json_part)
  end

  it "performs a complex trading simulation flow including news" do
    # --- 1. Начинаем сессию ---
    post "/mcp", params: { jsonrpc: "2.0", id: 1, method: "initialize" }.to_json, headers: { "Content-Type" => "application/json" }
    expect(response).to have_http_status(:success)
    session_id = response.headers["Mcp-Session-Id"]
    headers = { "Content-Type" => "application/json", "Mcp-Session-Id" => session_id }

    # --- 2. Ставим время на 12:00 ---
    post "/mcp", params: { 
      jsonrpc: "2.0", id: 2, method: "tools/call", 
      params: { name: "set_time", arguments: { timestamp: base_time.to_i } } 
    }.to_json, headers: headers
    expect(response.body).to include("Success")

    # --- 3. Добавляем 100,000 USDT ---
    post "/mcp", params: { 
      jsonrpc: "2.0", id: 3, method: "tools/call", 
      params: { name: "add_asset", arguments: { symbol: "USDT", amount: 100000 } } 
    }.to_json, headers: headers
    expect(response.body).to include("Success")

    # --- 4. Дергаем тулы: портфель, свечки, ордера (пусто) ---
    post "/mcp", params: { jsonrpc: "2.0", id: 4, method: "tools/call", params: { name: "get_portfolio" } }.to_json, headers: headers
    expect(JSON.parse(response.body)["result"]["content"][0]["text"]).to include("100000.0")

    # Проверяем новости (должны быть видны только те, что до 12:00)
    NewsItem.create!(title: "Morning News", published_at: base_time - 1.hour)
    NewsItem.create!(title: "Noon News", published_at: base_time + 1.minute) # Future relative to 12:00
    
    post "/mcp", params: { jsonrpc: "2.0", id: 41, method: "tools/call", params: { name: "get_news", arguments: { limit: 5 } } }.to_json, headers: headers
    news_text = JSON.parse(response.body)["result"]["content"][0]["text"]
    expect(news_text).to include("Morning News")
    expect(news_text).not_to include("Noon News")

    # --- 5. Плейсим ордер типа маркет - выполняется сразу ---
    post "/mcp", params: { 
      jsonrpc: "2.0", id: 6, method: "tools/call", 
      params: { name: "add_order", arguments: { pair: "BTCUSD", type: "buy", ordertype: "market", volume: 0.1 } } 
    }.to_json, headers: headers
    expect(response.body).to include("@ $60060.0")

    # --- 6. Плейсим ордер типа лимит - не выполняется сразу и виден в списке ---
    post "/mcp", params: { 
      jsonrpc: "2.0", id: 7, method: "tools/call", 
      params: { name: "add_order", arguments: { pair: "BTCUSD", type: "buy", ordertype: "limit", volume: 0.1, price: 59600 } } 
    }.to_json, headers: headers
    json = JSON.parse(response.body)
    order_id = json["result"]["content"][0]["text"].scan(/[a-f0-9]{8}/).last

    # --- 7. Делаем travel to + 1 minute ---
    # Симулируем раздумье модели. В это время цена BTC упала до 59,400
    travel_to(Time.current + 1.minute) do
      # Делаем список ордеров и портфель - видим эффект (ордер должен исполниться)
      post "/mcp", params: { jsonrpc: "2.0", id: 9, method: "tools/call", params: { name: "get_portfolio" } }.to_json, headers: headers
      portfolio = extract_portfolio(response.body)
      expect(portfolio["assets"]["BTCUSD"]["total"]).to eq(0.2) # 0.1 от маркета + 0.1 от лимитки

      # Теперь и новость Noon News должна быть видна
      post "/mcp", params: { jsonrpc: "2.0", id: 91, method: "tools/call", params: { name: "get_news" } }.to_json, headers: headers
      expect(JSON.parse(response.body)["result"]["content"][0]["text"]).to include("Noon News")
    end

    # --- 8. Мотаем время на час через mcp (13:00) ---
    post "/mcp", params: { 
      jsonrpc: "2.0", id: 11, method: "tools/call", 
      params: { name: "set_time", arguments: { timestamp: (base_time + 1.hour).to_i } } 
    }.to_json, headers: headers
    expect(response.body).to include("13:00")

    # --- 13. Финиш - фиксируем pnl ---
    post "/mcp", params: { jsonrpc: "2.0", id: 16, method: "tools/call", params: { name: "finish_trade" } }.to_json, headers: headers
    expect(response.body).to include("Final Equity")
  end

  it "performs 'Greedy Trader' scenario with automatic order cleanup on finish" do
    # 1. Start session
    post "/mcp", params: { jsonrpc: "2.0", id: 1, method: "initialize" }.to_json, headers: { "Content-Type" => "application/json" }
    headers = { "Content-Type" => "application/json", "Mcp-Session-Id" => response.headers["Mcp-Session-Id"] }
    post "/mcp", params: { jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "set_time", arguments: { timestamp: base_time.to_i } } }.to_json, headers: headers
    post "/mcp", params: { jsonrpc: "2.0", id: 3, method: "tools/call", params: { name: "add_asset", arguments: { symbol: "USDT", amount: 100000 } } }.to_json, headers: headers

    # 2. Market buy BTC and ETH
    post "/mcp", params: { jsonrpc: "2.0", id: 4, method: "tools/call", params: { name: "add_order", arguments: { pair: "BTCUSD", type: "buy", ordertype: "market", volume: 1 } } }.to_json, headers: headers
    post "/mcp", params: { jsonrpc: "2.0", id: 5, method: "tools/call", params: { name: "add_order", arguments: { pair: "ETHUSD", type: "buy", ordertype: "market", volume: 10 } } }.to_json, headers: headers

    # 3. Place Limit orders that won't be filled
    post "/mcp", params: { jsonrpc: "2.0", id: 6, method: "tools/call", params: { name: "add_order", arguments: { pair: "BTCUSD", type: "sell", ordertype: "limit", volume: 1, price: 100000 } } }.to_json, headers: headers
    post "/mcp", params: { jsonrpc: "2.0", id: 7, method: "tools/call", params: { name: "add_order", arguments: { pair: "ETHUSD", type: "buy", ordertype: "limit", volume: 10, price: 1000 } } }.to_json, headers: headers

    # 4. Check portfolio - funds should be reserved
    post "/mcp", params: { jsonrpc: "2.0", id: 8, method: "tools/call", params: { name: "get_portfolio" } }.to_json, headers: headers
    portfolio = extract_portfolio(response.body)
    expect(portfolio["assets"]["BTCUSD"]["available"]).to eq(0.0)
    expect(portfolio["assets"]["BTCUSD"]["reserved"]).to eq(1.0)

    # 5. Finish without cancelling
    post "/mcp", params: { jsonrpc: "2.0", id: 9, method: "tools/call", params: { name: "finish_trade" } }.to_json, headers: headers
    expect(response.body).to include("Final Equity")
    
    session = TradingSession.find_by(uuid: headers["Mcp-Session-Id"])
    expect(session.open_orders).to be_empty
  end
end
