require "rails_helper"

RSpec.describe RemoteTradingSessionPresenter do
  let(:mcp_url) { "http://trading-mcp.test/mcp" }
  let(:workdir_uuid) { SecureRandom.uuid }
  let(:mcp_session_id) { SecureRandom.uuid }

  let(:loop_record) do
    create(:solid_loop_loop,
      agent_class_name: "TradingAgent",
      state: { "workdir_uuid" => workdir_uuid }
    ).tap do |l|
      SolidLoop::McpSession.create!(loop: l, mcp_name: "trading", session_id: mcp_session_id)
    end
  end

  let(:presenter) { described_class.new(loop_record.id) }

  def stub_mcp_tool(tool_name, response_data)
    stub_request(:post, mcp_url)
      .with(
        body: hash_including("method" => "tools/call", "params" => hash_including("name" => tool_name)),
        headers: { "Mcp-Session-Id" => mcp_session_id }
      )
      .to_return(
        status: 200,
        body: {
          jsonrpc: "2.0", id: "1",
          result: { content: [{ type: "text", text: response_data.to_json }] }
        }.to_json,
        headers: { "Content-Type" => "application/json" }
      )
  end

  before do
    stub_const("ENV", ENV.to_h.merge("TRADING_MCP_URL" => mcp_url))
  end

  describe "#initial_balance" do
    context "when session has fills in history" do
      # API returns newest-first; chronologically first = .last
      let(:history_response) do
        {
          "trades" => [
            {
              "status" => "fill", "side" => "sell", "pair" => "BTC-USD",
              "timestamp" => "2026-04-10T08:00:00Z",
              "fill" => { "qty" => "0.01", "price" => "72500.0", "fee_value" => "0.73" },
              "portfolio" => { "value_after" => "10010.50" }
            },
            {
              "status" => "fill", "side" => "buy", "pair" => "BTC-USD",
              "timestamp" => "2026-04-09T22:30:00Z",
              "fill" => { "qty" => "0.01", "price" => "72000.0", "fee_value" => "0.72" },
              "portfolio" => { "value_after" => "9985.28" }
            }
          ],
          "count" => 2
        }
      end

      before { stub_mcp_tool("trade/get_history", history_response) }

      it "returns equity_before of the first chronological fill" do
        # first fill = fills.last = value_after(9985.28) + fee(0.72) = 9986.00
        expect(presenter.initial_balance).to eq(9986.0)
      end

      it "does not use day_start_value which resets at midnight" do
        # day_start_value would be ~9984 after midnight reset — must not be used
        expect(presenter.initial_balance).to be > 9985.0
      end
    end

    context "when there are no fills yet" do
      before do
        stub_mcp_tool("trade/get_history", { "trades" => [], "count" => 0 })
        stub_mcp_tool("trade/get_limits", {
          "portfolio" => { "day_start_value" => "10000", "value" => "10000", "cash" => "10000" }
        })
      end

      it "falls back to day_start_value from get_limits" do
        expect(presenter.initial_balance).to eq(10_000.0)
      end
    end
  end

  describe "#pnl" do
    before do
      stub_mcp_tool("trade/get_history", {
        "trades" => [
          {
            "status" => "fill", "side" => "buy", "pair" => "BTC-USD",
            "timestamp" => "2026-04-09T22:30:00Z",
            "fill" => { "qty" => "0.01", "price" => "72000.0", "fee_value" => "0.72" },
            "portfolio" => { "value_after" => "9985.28" }
          }
        ],
        "count" => 1
      })
      stub_mcp_tool("trade/get_portfolio", {
        "portfolio" => { "value" => "9982.68", "cash" => "9231.70" },
        "positions" => [{ "pair" => "BTC-USD", "qty" => "0.0137", "value" => "751.0" }]
      })
    end

    it "is negative when current equity is below initial balance" do
      # initial = 9985.28 + 0.72 = 9986.0, equity = 9982.68 → pnl = -3.32
      expect(presenter.pnl).to be < 0
      expect(presenter.pnl).to be_within(0.01).of(9982.68 - 9986.0)
    end
  end
end
