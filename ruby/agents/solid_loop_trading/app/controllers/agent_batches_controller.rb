class AgentBatchesController < ApplicationController
  before_action :set_agent_batch, only: [ :show, :start, :stop, :restart_errors, :destroy, :warm_up, :recalculate_stats, :download, :stop_orchestrator, :resume_orchestrator ]

  def index
    @agent_batches = AgentBatch.order(created_at: :desc)
  end

  def new
    @agent_batch = AgentBatch.new
    @trading_scenarios = TradingScenario.all

    # Pre-fill defaults from the first task if available
    if (first_task = TradingScenario.first)
      @agent_batch.start_at = first_task.start_at
      @agent_batch.end_at = first_task.end_at
      @agent_batch.step_interval = first_task.step_interval
    end
  end

  def create
    @agent_batch = AgentBatch.new(agent_batch_params)
    @agent_batch.status = :init

    task_ids = params[:agent_batch][:trading_scenario_ids].reject(&:blank?)

    if task_ids.empty?
      @agent_batch.destroy
      @trading_scenarios = TradingScenario.all
      flash.now[:alert] = "You must select at least one scenario."
      render :new, status: :unprocessable_content
      return
    end

    if AgentBatch::Create.call(agent_batch: @agent_batch, trading_scenario_ids: task_ids)
      redirect_to agent_batches_path, notice: "Batch created in INIT state."
    else
      @trading_scenarios = TradingScenario.all
      render :new, status: :unprocessable_content
    end
  end

  def show
    @onboarding_step = nil

    @progress = @agent_batch.progress_stats
    @has_errors = @agent_batch.loops.where(status: :failed).any?

    result = AgentRunsQuery.new(@agent_batch.agent_runs).call(params)
    @agent_runs = result.executions

    @token_stats = @agent_batch.loops.pick(
      Arel.sql("SUM(cost), SUM(tokens_prompt), SUM(tokens_completion), SUM(tokens_total)")
    ).then do |cost, input, output, total|
      { cost: cost.to_f, input: input.to_i, output: output.to_i, total: total.to_i }
    end

    pnl_values = @agent_batch.agent_runs
      .includes(:trading_scenario, loop: :mcp_sessions)
      .filter_map do |run|
        mcp_session = run.loop&.mcp_sessions&.find { |s| s.mcp_name == "trading" }
        next unless mcp_session

        if run.live?
          begin
            presenter = RemoteTradingSessionPresenter.new(run.loop.id)
            presenter.pnl
          rescue => e
            Rails.logger.warn "[BatchShow] PNL fetch failed for run #{run.id}: #{e.message}"
            next
          end
        else
          ts = TradingSession.find_by(uuid: mcp_session.session_id)
          next unless ts
          ts.total_equity_usdt - run.trading_scenario&.initial_balance.to_f
        end
      end

    @pnl_stats = pnl_values.any? ? { total: pnl_values.sum, min: pnl_values.min, max: pnl_values.max, count: pnl_values.size } : nil
  end

  def stop
    @agent_batch.stop!
    redirect_to @agent_batch, notice: "Batch stopped."
  end

  def start
    if @agent_batch.init? || @agent_batch.stopped?
      @agent_batch.update!(status: :started)
      @agent_batch.check_and_start_next!

      redirect_to agent_batch_path(@agent_batch), notice: "Batch started."
    else
      redirect_to agent_batch_path(@agent_batch), alert: "Batch already started or completed."
    end
  end

  def restart_errors
    @agent_batch.restart_errors!
    redirect_to @agent_batch, notice: "Restarting executions that ended with error."
  end

  def stop_orchestrator
    @agent_batch.stop_orchestrator!
    redirect_to @agent_batch, notice: "All agents paused. Orchestrator will not wake them up until resumed."
  end

  def resume_orchestrator
    @agent_batch.resume_orchestrator!
    redirect_to @agent_batch, notice: "All agents resumed. Orchestrator will wake them up on next heartbeat or alert."
  end

  def warm_up
    BenchmarkWarmUpJob.perform_later(@agent_batch.id)
    redirect_to @agent_batch, notice: "Model warm-up started."
  end
  def recalculate_stats
    # @agent_batch.agent_runs.each do |execution|
    #   execution.recalculate_stats!(inline: true)
    # end
    redirect_to @agent_batch, notice: "Stats recalculation disabled until metrics port is complete."
  end

  def download
    @agent_runs = @agent_batch.agent_runs.order(created_at: :asc)

    html_content = render_to_string(
      template: "agent_batches/download",
      layout: false
    )

    safe_prefix = @agent_batch.prefix.to_s.parameterize
    filename = "batch-#{@agent_batch.id}-#{safe_prefix}-#{Time.current.strftime('%Y%m%d%H%M')}.html"

    send_data html_content, filename: filename, type: "text/html", disposition: "attachment"
  end

  def destroy
    expected_name = "#{@agent_batch.agent_model&.name}/#{@agent_batch.prefix}"

    if params[:confirmation] == expected_name
      @agent_batch.destroy
      redirect_to agent_batches_path, notice: "Batch deleted."
    else
      redirect_to @agent_batch, alert: "Deletion failed. Confirmation text was incorrect. Expected: '#{expected_name}'"
    end
  end

  private

  def set_agent_batch
    @agent_batch = AgentBatch.find(params[:id])
  end

  def agent_batch_params
    params.require(:agent_batch).permit(:agent_model_id, :concurrency, :attempts, :prefix, :start_at, :end_at, :step_interval, :agent_mode, :wakeup_interval_minutes)
  end
end
