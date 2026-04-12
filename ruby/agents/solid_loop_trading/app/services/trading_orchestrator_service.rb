class TradingOrchestratorService
  def self.process_turn_completion(message)
    return unless message.role == "assistant" && message.tool_calls.empty?

    return if message.content.include?("Portfolio liquidated")

    return if message.loop.reload.failed?

    execution = message.loop.subject
    return unless execution.is_a?(AgentRun) && execution.trading_scenario&.category == "trading"

    return if execution.orchestrator_stopped?

    new(execution).determine_next_move(message.loop, message)
  end

  def initialize(execution)
    @execution = execution
    @task = execution.trading_scenario
  end

  def determine_next_move(loop_record, summary_message)
    tombstone_turn!(loop_record, summary_message)

    current_ts = loop_record.state["virtual_time"] || @execution.start_at.to_i
    current_time = Time.at(current_ts).utc
    next_time = current_time + @execution.step_interval.seconds

    if next_time >= @execution.end_at
      finalize_simulation(loop_record)
    else
      @execution.orchestrator_running!
      advance_simulation(loop_record, next_time)
    end
  end

  private

  COMPACTION_THRESHOLD      = 100
  KEEP_FULL_LAST_N          = 24  # last 24 summaries (today): keep all
  YESTERDAY_WINDOW          = 24  # next 24 summaries (yesterday): thin to every 4th → ~6
  YESTERDAY_KEEP_EVERY_NTH  = 4
  OLDER_KEEP_EVERY_NTH      = 24  # older: one per day

  def tombstone_turn!(loop_record, summary_message)
    summary_message.update!(metadata: (summary_message.metadata || {}).merge("turn_summary" => true))

    return if loop_record.messages.where(is_hidden: false).count < COMPACTION_THRESHOLD

    # Step 1: hide all intermediate messages from completed turns
    # (keep: system, turn summaries, current summary)
    loop_record.messages
      .where(is_hidden: false)
      .where.not(role: "system")
      .where("(metadata->>'turn_summary') IS DISTINCT FROM 'true'")
      .where.not(id: summary_message.id)
      .update_all(is_hidden: true)

    # Step 2: thin old summaries — last day full, yesterday sparse, older one-per-day
    all_summaries = loop_record.messages
      .where("(metadata->>'turn_summary') = 'true'")
      .order(id: :desc)
      .to_a

    keep_ids = select_summaries_to_keep(all_summaries).map(&:id).to_set

    all_summaries.each do |s|
      hidden = !keep_ids.include?(s.id)
      s.update_column(:is_hidden, hidden) if s.is_hidden != hidden
    end
  end

  # summaries: ordered newest-first
  def select_summaries_to_keep(summaries)
    summaries.select.with_index do |_, i|
      if i < KEEP_FULL_LAST_N
        true
      elsif i < KEEP_FULL_LAST_N + YESTERDAY_WINDOW
        (i - KEEP_FULL_LAST_N) % YESTERDAY_KEEP_EVERY_NTH == 0
      else
        (i - KEEP_FULL_LAST_N - YESTERDAY_WINDOW) % OLDER_KEEP_EVERY_NTH == 0
      end
    end
  end

  def advance_simulation(loop_record, next_time)
    call_trading_tool(loop_record, "set_time", { timestamp: next_time.to_i })

    # 2. Get fresh portfolio status to show the agent
    portfolio_json = call_trading_tool(loop_record, "get_portfolio")

    # 3. Update the state tracker
    loop_record.state["virtual_time"] = next_time.to_i
    loop_record.save!

    prompt = <<~MSG
      Market Update — #{next_time.utc}

      #{portfolio_json}

      Review the latest news and price action, then execute your trades or hold.
    MSG

    loop_record.messages.create!(role: "user", content: prompt)

    # Re-open the loop for the next turn
    loop_record.update!(status: "queued")
    SolidLoop::LlmCompletionJob.perform_later(loop_record.id)
  end

  def finalize_simulation(loop_record)
    @execution.orchestrator_stopped!

    result_text = call_trading_tool(loop_record, "finish_trade")
    final_equity = result_text.scan(/[\d.]+/).last.to_f

    # Update the execution record
    @execution.update_columns(score: final_equity, status: "completed")

    loop_record.messages.create!(role: "user", content: result_text)
    loop_record.update!(status: "completed")
  end

  def call_trading_tool(loop_record, tool_name, arguments = {})
    agent_instance = @execution.agent
    SolidLoop::McpSessionInitializer.call(loop_record, agent_instance)
    mcp_session = loop_record.mcp_sessions.find_by!(mcp_name: "trading")
    mcp_tool = mcp_session.mcp_tools.find_or_create_by!(name: tool_name)
    mcp_tool.call(agent: agent_instance, arguments: arguments)[:result]
  end
end
