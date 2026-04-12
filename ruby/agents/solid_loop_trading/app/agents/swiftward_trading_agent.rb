class SwiftwardTradingAgent < SolidLoop::Base
  # Sessions routed through Swiftward gateway (guarded, stateless proxy)
  GUARDED_SESSIONS = %w[trading market].freeze
  # Sessions connected directly to trading-server (stateful, no guardrails)
  DIRECT_SESSIONS  = %w[files code news].freeze

  TRADING_TOOLS = %w[
    trade/submit_order
    trade/estimate_order
    trade/get_portfolio
    trade/get_history
    trade/get_portfolio_history
    trade/get_limits
    trade/heartbeat
  ].freeze

  MARKET_TOOLS = %w[
    market/get_prices
    market/get_candles
    market/get_orderbook
    market/list_markets
  ].freeze

  FILES_TOOLS = %w[
    files/read
    files/write
    files/edit
    files/append
    files/delete
    files/list
    files/find
    files/search
  ].freeze

  CODE_TOOLS = %w[
    code/execute
  ].freeze

  NEWS_TOOLS = %w[
    news/search
    news/get_latest
    news/get_sentiment
    news/set_alert
    news/get_triggered_alerts
    news/get_events
  ].freeze

  def llm_provider
    model = subject.agent_model
    provider = model.provider
    {
      api_token: provider.api_key,
      base_url: provider.base_url,
      model: model.llm_model_name
    }
  end

  def tools
    []
  end

  def mcps
    [
      # Guarded via Swiftward — stateless proxy, no session header returned
      {
        name: :trading,
        url: "#{swiftward_base_url}/mcp/trading",
        api_token: ENV["SWIFTWARD_API_KEY"],
        custom_headers: swiftward_headers,
        stateless: true,
        tools: TRADING_TOOLS
      },
      {
        name: :market,
        url: "#{swiftward_base_url}/mcp/market",
        api_token: ENV["SWIFTWARD_API_KEY"],
        custom_headers: swiftward_headers,
        stateless: true,
        tools: MARKET_TOOLS
      },
      # Direct to trading-server — stateful, session ID required
      {
        name: :files,
        url: "#{trading_server_url}/mcp/files",
        custom_headers: swiftward_headers,
        tools: FILES_TOOLS
      },
      {
        name: :code,
        url: "#{trading_server_url}/mcp/code",
        custom_headers: swiftward_headers,
        tools: CODE_TOOLS
      },
      {
        name: :news,
        url: "#{trading_server_url}/mcp/news",
        custom_headers: swiftward_headers,
        tools: NEWS_TOOLS
      }
    ]
  end

  def on_message_created(message)
    AgentCostService.apply_if_missing(message, agent_model: subject.agent_model, loop_record: loop_record)
  end

  def refresh_mcp_tools?
    all_sessions = GUARDED_SESSIONS + DIRECT_SESSIONS
    sessions = loop_record.mcp_sessions.where(mcp_name: all_sessions)
    return true if sessions.count < all_sessions.size

    sessions.any? { |s| s.mcp_tools.empty? }
  end

  def system_prompt
    <<~PROMPT
      #{subject.trading_scenario.prompt}

      You are operating through the Swiftward policy gateway. The gateway automatically enforces risk guardrails on every order — do not attempt to circumvent limits; violating orders will be rejected before reaching the exchange.

      TOOLS:
      - trade/* — order submission and portfolio management: submit_order, estimate_order, get_portfolio, get_history, get_portfolio_history, get_limits, heartbeat
      - market/* — read-only market data: get_prices, get_candles, get_orderbook, list_markets
      - files/* — persistent file storage: read, write, edit, append, delete, list, find, search
      - code/* — Python code execution sandbox: execute
      - news/* — market news and sentiment: search, get_latest, get_sentiment, set_alert, get_triggered_alerts, get_events

      TRADING RULES:
      1. At the start of each turn, call trade/get_limits and trade/get_portfolio to understand your current risk headroom and positions.
      2. Before submitting an order, call trade/estimate_order to verify it will pass guardrails.
      3. Use limit orders when possible (lower fees and tighter price control).
      4. Every order must include stop_loss and take_profit parameters — the gateway enforces maximum stop-loss distance.
      5. When you are done trading for this period, write a concise session summary as your final message (no tool calls): current positions, key price levels, what you did and why, and your plan for next period. This summary is your memory — make it count.

      FILES:
      Keep a persistent journal.md — read it at the start of each turn and append your observations at the end. Write reusable analysis scripts and improve them each turn.
    PROMPT
  end

  private

  def swiftward_base_url
    ENV["SWIFTWARD_MCP_URL"] || "http://localhost:8095"
  end

  def trading_server_url
    ENV["TRADING_SERVER_URL"] || "http://localhost:8091"
  end

  def agent_id
    agent_run = loop_record.subject
    unless agent_run.agent_id
      agent_run.update!(agent_id: SecureRandom.uuid)
    end
    agent_run.agent_id
  end

  def swiftward_headers
    { "X-Agent-ID" => agent_id }
  end
end
