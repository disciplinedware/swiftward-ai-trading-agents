class AgentRunsController < ApplicationController
  before_action :set_agent_run, only: [ :show, :pause, :resume, :stop_agent, :resume_agent, :edit, :update, :destroy, :download, :mcp, :run_mcp_tool, :tools ]

  def index
    result = AgentRunsQuery.new.call(params)
    @agent_runs = result.executions
  end

  def show
    # @agent_run.recalculate_stats! # Inline recalculation (TODO: port to Loop)
    @solid_loop = @agent_run.loop
    @messages = @solid_loop&.messages&.includes(:tool_calls)&.order(id: :asc)&.to_a || []
  end

  def edit
  end

  def update
    if @agent_run.update(agent_run_params)
      redirect_to @agent_run, notice: "Execution settings were successfully updated."
    else
      render :edit, status: :unprocessable_content
    end
  end

  def new
    @agent_run = AgentRun.new
  end

  def create
    @agent_run = AgentRun.new(agent_run_params)

    system_content = params[:system_prompt]

    if system_content.present? && @agent_run.filename.present?
      system_content = system_content.gsub("{{FILE_NAME}}", @agent_run.filename)
    end

    @agent_run.system_prompt = system_content
    @agent_run.user_prompt = params[:user_prompt]

    if @agent_run.save
      if params[:user_prompt].present?
        agent_class_name = params[:agent_class_name].presence || "TradingAgent"
        agent_class_name.constantize.start!(
          subject: @agent_run,
          message: params[:user_prompt],
          filename: @agent_run.filename
        )
      end

      redirect_to @agent_run, notice: "Execution was successfully started."
    else
      render :new, status: :unprocessable_content
    end
  end

  def pause
    @agent_run.agent&.pause!
    redirect_to @agent_run
  end

  def resume
    if @agent_run.agent&.resume!
      redirect_to @agent_run, notice: "Execution resumed."
    else
      redirect_to @agent_run, alert: "Could not resume execution."
    end
  end

  def stop_agent
    @agent_run.orchestrator_stopped!
    redirect_to @agent_run, notice: "Agent stopped. It will no longer be woken up by the orchestrator."
  end

  def resume_agent
    @agent_run.orchestrator_waiting!
    redirect_to @agent_run, notice: "Agent resumed. Orchestrator will wake it up on next alert or heartbeat."
  end

  def destroy
    @agent_run.destroy
    redirect_to agent_runs_url, notice: "Execution was successfully deleted."
  end

  def download
    @solid_loop = @agent_run.loop
    @messages = @solid_loop&.messages&.includes(:tool_calls)&.order(id: :asc) || []

    html_content = render_to_string(
      template: "agent_runs/download",
      layout: false,
      locals: { execution: @agent_run, messages: @messages }
    )

    filename = "execution-#{@agent_run.id}-#{Time.current.strftime('%Y%m%d%H%M')}.html"
    send_data html_content, filename: filename, type: "text/html", disposition: "attachment"
  end

  def recalculate_all
    AgentRun.find_each do |execution|
      # AgentStatsRecalculationJob.perform_later(execution.id) (TODO: port metrics)
    end
    redirect_to agent_runs_path, notice: "Recalculation of all statistics started in background."
  end

  def mcp
    @agent_model = @agent_run.agent_model
    @session_id = params[:session_id]

    if params[:event_id].present?
      event = SolidLoop::Event.find(params[:event_id])
      tool_call = event.eventable
      @replay_tool_name = tool_call.function_name
      @replay_arguments = tool_call.arguments
      @session_id ||= tool_call.mcp_session&.session_id
    end

    if @session_id.present?
      @mcp_session = @agent_run.loop&.mcp_sessions&.find_by(session_id: @session_id)
      if @mcp_session
        @tools = @mcp_session.mcp_tools.map do |tool|
          {
            "name" => tool.name,
            "description" => tool.description,
            "inputSchema" => tool.input_schema
          }
        end
      end
    end

    # Fallback to native tools if no session tools found or session not specified
    if @tools.nil? || @tools.empty?
      @tools = (@agent_model.tools || []).map do |tool_item|
        klass = tool_item.to_s.constantize
        {
          "name" => klass::FUNCTION_NAME,
          "description" => klass::FUNCTION_DESCRIPTION,
          "inputSchema" => klass::FUNCTION_PARAMETERS
        }
      end
    end
  end

  def tools
    @solid_loop = @agent_run.loop
    @tool_calls = @solid_loop&.tool_calls&.order(id: :asc) || []
  end

  def run_mcp_tool
    tool_name = params[:tool_name]
    session_id = params[:session_id]
    arguments = params[:arguments]&.to_unsafe_h || {}

    result_text = "Error: Tool or Session not found"
    duration = 0.0
    log = ""

    if session_id.present?
      @mcp_session = @agent_run.loop&.mcp_sessions&.find_by(session_id: session_id)
      @mcp_tool = @mcp_session&.mcp_tools&.find_by(name: tool_name)

      if @mcp_tool
        result_hash = @mcp_tool.call(
          agent: @agent_run.agent,
          arguments: arguments
        )
        result_text = result_hash[:result]
        duration = result_hash[:duration]
        log = result_hash[:log]
      end
    end

    render turbo_stream: turbo_stream.update("mcp_output",
      partial: "agent_runs/mcp_output",
      locals: { result: result_text, duration: duration, log: log }
    )
  end

  private

  def set_agent_run
    @agent_run = AgentRun.find(params[:id])
  end

  def agent_run_params
    params.require(:agent_run).permit(:title, :agent_model_id, :filename)
  end
end
