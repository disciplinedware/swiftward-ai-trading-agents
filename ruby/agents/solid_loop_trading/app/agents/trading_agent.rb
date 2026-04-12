class TradingAgent < SolidLoop::Base
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

  SANDBOX_TOOLS = %w[write_file shell read_file list_files grep_file get_file_info edit_file].freeze


  def mcps
    unless loop_record.state["workdir_uuid"]
      loop_record.state["workdir_uuid"] = SecureRandom.uuid
      loop_record.save!
    end

    uuid = loop_record.state["workdir_uuid"]

    [
      {
        name: :trading,
        url: ENV["TRADING_MCP_URL"] || "http://localhost:3000/mcp",
        tools: %w[get_candles add_order cancel_order get_open_orders get_portfolio get_news get_order_history finish_trade],
        initialize_params: {
          initializationOptions: { workdir_uuid: uuid }
        }
      },
      {
        name: :sandbox,
        url: ENV["MCP_SERVER"] || "http://localhost:9091",
        api_token: ENV["MCP_API_TOKEN"],
        custom_headers: { "X-Sandbox-Workdir-UUID" => uuid },
        tools: SANDBOX_TOOLS
      }
    ]
  end

  def refresh_mcp_tools?
    # Ensure both sessions exist and each has its tools populated
    session_names = %w[trading sandbox]
    return true if loop_record.mcp_sessions.where(mcp_name: session_names).count < session_names.size

    # Check if any session is missing its tools
    loop_record.mcp_sessions.where(mcp_name: session_names).any? { |s| s.mcp_tools.empty? }
  end

  def on_mcp_session_initialized(mcp_session)
    return unless mcp_session.mcp_name == "trading"
    return if loop_record.state["trading_setup_done"]

    execution = subject
    return unless execution.is_a?(AgentRun) && execution.trading_scenario&.category == "trading"

    Rails.logger.info "[TradingAgent] Initializing trading session via hook for Loop ##{loop_record.id}"
    task = execution.trading_scenario

    # Initialize state
    loop_record.state["virtual_time"] = task.start_at.to_i

    # Perform secret setup
    call_mcp(mcp_session, "set_time", { timestamp: task.start_at.to_i })
    call_mcp(mcp_session, "add_asset", { symbol: "USDT", amount: task.initial_balance.to_f })

    # Mark as done
    loop_record.state["trading_setup_done"] = true
    loop_record.save!
  end

  def on_message_created(message)
    TradingOrchestratorService.process_turn_completion(message)
    AgentCostService.apply_if_missing(message, agent_model: subject.agent_model, loop_record: loop_record)
  end

  def system_prompt
    tickers = OhlcCandle::TICKERS.join(", ")
    <<~PROMPT
      #{subject.trading_scenario.prompt}

      EXCHANGE RULES:
      1. You are a multi-asset hedge fund manager. Available pairs: #{tickers}.
      2. You receive periodic market updates — analyze each one and act accordingly.
      3. Always call 'get_portfolio' before making trading decisions.
      4. Use 'add_order' for trades. Limit orders are preferred (lower fees).
      5. When you are done trading for this period, write a concise session summary as your final message (no tool calls): current positions, key price levels, what you did and why, and your plan for next period. This summary is your memory — make it count.

      SANDBOX:
      You have a persistent code execution sandbox. Use it to build analytical tools over time.
      - 'get_candles' saves CSV files directly into your sandbox — use 'list_files' to see them.
      - Write scripts with 'write_file' and run them with 'shell'. Reuse and improve them each turn.
      - Keep a journal.md — read it at the start of each turn, append your observations at the end.
    PROMPT
  end

  private

  def sandbox_dir
    uuid = loop_record.state["workdir_uuid"] || "trading_#{loop_record.id}"
    Rails.root.join("storage", "external", "data", uuid).to_s
  end

  def call_mcp(session_record, tool_name, arguments)
    mcp_tool = session_record.mcp_tools.find_or_create_by!(name: tool_name)
    SolidLoop::McpToolExecutionService.call(
      mcp_tool: mcp_tool,
      agent: self,
      arguments: arguments
    )
  end
end
