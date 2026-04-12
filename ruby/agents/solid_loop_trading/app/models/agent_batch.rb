class AgentBatch < ApplicationRecord
  AGENT_MODES = %w[backtesting swiftward].freeze

  belongs_to :agent_model
  has_many :agent_runs, dependent: :destroy
  has_many :loops, through: :agent_runs, source: :loop

  validates :agent_model, presence: true
  validates :agent_mode, inclusion: { in: AGENT_MODES }
  validates :concurrency, presence: true, numericality: { greater_than: 0 }
  validates :attempts, presence: true, numericality: { greater_than: 0 }
  validates :wakeup_interval_minutes, numericality: { greater_than: 0 }

  enum :warm_up_status, {
    not_started: 0,
    pending: 1,
    executing: 2,
    warmed_up: 3
  }, prefix: :warm_up, default: :not_started

  enum :status, {
    init: "init",
    started: "started",
    error: "error",
    completed: "completed",
    stopped: "stopped"
  }, default: :init

  def aggregate_stats
    completed_loops = loops.where(status: [ :completed, :failed ])
    total_loops = loops
    run_count = agent_runs.count

    success_count = completed_loops.where(status: :completed).count
    success_rate = completed_loops.count > 0 ? (success_count.to_f / completed_loops.count * 100).round(1) : 0

    total_tokens = total_loops.sum(:tokens_total)
    total_duration_model = total_loops.sum(:duration_generation)
    total_duration_tools = total_loops.sum(:duration_tools)

    avg_tps = if total_loops.sum(:duration_total) > 0
      (total_loops.sum(:tokens_total).to_f / total_loops.sum(:duration_total)).round(2)
    else
      0.0
    end

    tool_calls_count = SolidLoop::ToolCall.joins(:message).where(solid_loop_messages: { loop_id: total_loops.ids }).count
    messages_count = SolidLoop::Message.where(loop_id: total_loops.ids).count
    tool_reliance = messages_count > 0 ? (tool_calls_count.to_f / messages_count).round(2) : 0

    {
      success_rate: success_rate,
      avg_tps: avg_tps,
      total_tokens: total_tokens,
      total_duration_model: total_duration_model,
      total_duration_tools: total_duration_tools,
      tool_reliance: tool_reliance
    }
  end

  def progress_stats
    total = agent_runs.count
    completed = loops.where(status: [ :completed, :failed ]).count
    stats = {
      total: total,
      completed: completed,
      percent: total > 0 ? (completed.to_f / total * 100).round(1) : 0
    }

    if agent_mode == "swiftward"
      stats[:running] = agent_runs.orchestrator_running.count
      stats[:waiting] = agent_runs.orchestrator_waiting.count
      stats[:stopped] = agent_runs.orchestrator_stopped.count
    else
      stats[:solved] = loops.where(status: :completed).count
    end

    stats
  end

  def check_and_start_next!
    with_lock do
      return if completed? || error?

      # Find how many are active (idle or processing)
      active_count = loops.where(status: [ :queued, :running ]).count

      if active_count < concurrency
        # Need to start more
        to_start_count = concurrency - active_count
        next_paused = agent_runs.joins(:loop).where(solid_loop_loops: { status: [ :init, :paused, :queued ] }).limit(to_start_count)

        if next_paused.any?
          next_paused.each { |execution| execution.agent&.resume! }
        elsif active_count == 0
          # Nothing more to start and nothing active
          # Check if any conversations ended in error
          if loops.where(status: :failed).any?
            error!
          else
            completed!
          end
        end
      end
    end
  end

  def stop_orchestrator!
    agent_runs.where(orchestrator_status: [ :initial, :running, :waiting ]).update_all(orchestrator_status: "stopped")
  end

  def resume_orchestrator!
    agent_runs.orchestrator_stopped.update_all(orchestrator_status: "waiting")
  end

  def restart_errors!
    with_lock do
      return unless error? || started? || completed? || stopped?

      transaction do
        loops.where(status: :failed).each do |l|
          l.update!(status: :queued, error_message: nil)
        end
        started! if error? || completed? || stopped?
        check_and_start_next!
      end
    end
  end

  def stop!
    with_lock do
      return unless started?

      transaction do
        stopped!
        loops.where(status: [ :processing, :waiting_for_user ]).each do |l|
          l.update!(status: :paused)
          if l.state.is_a?(Hash)
            l.state["error_message"] = "Batch stopped"
            l.save!
          end
        end
      end
    end
  end
end
