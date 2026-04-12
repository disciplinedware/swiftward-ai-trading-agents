class AgentOrchestratorJob < ApplicationJob
  queue_as :default

  def perform
    AgentRun.orchestrator_waiting.find_each do |agent_run|
      next unless agent_run.live?

      process_waiting_agent(agent_run)
    end
  end

  private

  def process_waiting_agent(agent_run)
    loop_record = agent_run.loop
    return unless loop_record

    alerts = fetch_alerts(loop_record, agent_run)

    if alerts.any?
      Rails.logger.info "[AgentOrchestrator] #{alerts.size} alert(s) for agent_run #{agent_run.id}, waking up"
      start_new_loop(agent_run, build_alert_message(alerts))
    elsif should_heartbeat?(agent_run)
      Rails.logger.info "[AgentOrchestrator] Heartbeat for agent_run #{agent_run.id} (interval: #{agent_run.wakeup_interval_minutes}m)"
      start_new_loop(agent_run, "Heartbeat check: analyze current market status and update your trading decisions.")
    end
  end

  def fetch_alerts(loop_record, agent_run)
    mcp_session = loop_record.mcp_sessions.find_by(mcp_name: "news")
    return [] unless mcp_session

    mcp_tool = mcp_session.mcp_tools.find_by(name: "news/get_triggered_alerts")
    return [] unless mcp_tool

    result = SolidLoop::McpToolExecutionService.call(
      mcp_tool: mcp_tool,
      agent:     loop_record.agent,
      arguments: {}
    )

    return [] unless result[:success]

    data = JSON.parse(result[:result])
    data["alerts"] || []
  rescue JSON::ParserError => e
    Rails.logger.warn "[AgentOrchestrator] Bad JSON from news MCP for agent_run #{agent_run.id}: #{e.message}"
    []
  end

  def should_heartbeat?(agent_run)
    return true if agent_run.last_active_at.nil?

    Time.current - agent_run.last_active_at >= agent_run.wakeup_interval_minutes.minutes
  end

  def build_alert_message(alerts)
    lines = alerts.map { |a| "- #{a['title'].presence || a['message'].presence || a.to_json}" }
    "The following market alerts were triggered:\n#{lines.join("\n")}\n\nAnalyze these alerts and update your trading strategy accordingly."
  end

  def start_new_loop(agent_run, message)
    agent_run.orchestrator_running!

    loop_record = agent_run.loop
    unless loop_record
      Rails.logger.error "[AgentOrchestrator] No loop found for agent_run #{agent_run.id}, skipping"
      return
    end

    # Append a new user message to the existing conversation and re-queue the loop.
    # This keeps all sessions in one Loop record (matching has_one :loop on AgentRun)
    # and gives the LLM full conversation history as context on each wakeup.
    loop_record.messages.create!(role: "user", content: message)
    loop_record.update!(status: "queued", error_message: nil)
    SolidLoop::LlmCompletionJob.perform_later(loop_record.id)
  end
end
