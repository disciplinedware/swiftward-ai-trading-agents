require "rails_helper"
require "support/streaming_llm_emulator"
require "support/trading_mcp_stub"
require "support/mcp_emulator"

RSpec.describe "Trading Loop Simulation", type: :request do
  let(:base_url) { "http://llama-stream-server.local" }
  let(:api_token) { "trading-token" }
  let(:mcp_url) { "http://trading-mcp.local/mcp" }

  let(:provider) { create(:agent_model_provider, base_url: base_url, api_key: api_token) }
  let(:agent_model) { create(:agent_model, provider: provider, llm_model_name: "test-model") }

  let(:trading_scenario) do
    create(:trading_scenario,
      category: "trading",
      prompt: "Trade BTCUSD",
      start_at: Time.parse("2026-03-01 12:00:00 UTC"),
      end_at: Time.parse("2026-03-01 13:30:00 UTC"),
      initial_balance: 10000,
      step_interval: 3600
    )
  end
  let(:execution) { create(:agent_run, agent_model: agent_model, trading_scenario: trading_scenario) }
  let(:llm_emulator) { StreamingLlmEmulator.new(base_url, api_token) }

  # Создаем свечи через фабрику
  let!(:candle_12h) { create(:ohlc_candle, :btc, timestamp: trading_scenario.start_at, price: 50000) }
  let!(:candle_13h) { create(:ohlc_candle, :btc, timestamp: trading_scenario.start_at + 1.hour, price: 51000) }

  before do
    ENV["TRADING_MCP_URL"] = mcp_url
    ENV["MCP_SERVER"] = "http://sandbox-mcp.local"
    TradingMcpStub.stub_all(mcp_url)
    McpEmulator.new("http://sandbox-mcp.local", expected_filename: nil)

    # 12:00
    llm_emulator.set_answer(/Trade BTCUSD/, {
      role: "assistant", content: "Buy",
      tool_calls: [{ id: "tc1", type: "function", function: { name: "add_order", arguments: { pair: "BTCUSD", type: "buy", ordertype: "market", volume: 0.1 }.to_json } }]
    })
    # Ответ на результат покупки
    llm_emulator.set_answer(/executed/, { role: "assistant", content: "Wait" })

    # 13:00
    llm_emulator.set_answer(/Market Update/, {
      role: "assistant", content: "Finish",
      tool_calls: [{ id: "tc3", type: "function", function: { name: "add_order", arguments: { pair: "BTCUSD", type: "sell", ordertype: "market", volume: 0.05 }.to_json } }]
    })
    # Ответ на результат продажи (второй вызов со словом executed)
    llm_emulator.set_answer(/executed/, { role: "assistant", content: "Done" })
    llm_emulator.set_answer(/Portfolio liquidated/, { role: "assistant", content: "Done" })
  end

  it "automatically advances time and performs a 2-step simulation" do
    loop_record = TradingAgent.start!(subject: execution, message: "Trade BTCUSD")

    messages = loop_record.messages.order(:id).to_a
    expect(messages.map(&:content).join).to include("Market Update")
    expect(messages.map(&:content).join).to include("Done")

    execution.reload
    expect(execution.status).to eq("completed")
    expect(execution.score).to be_present

    expect(execution.loop.mcp_sessions.count).to eq(2)
    expect(execution.loop.mcp_sessions.find_by(mcp_name: 'trading').mcp_tools.size).to eq(10)
    expect(execution.loop.mcp_sessions.find_by(mcp_name: 'sandbox').mcp_tools.size).to eq(7)
  end
end
