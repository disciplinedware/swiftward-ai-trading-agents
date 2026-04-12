class AgentRun < ApplicationRecord
  belongs_to :agent_model
  belongs_to :agent_batch, optional: true
  belongs_to :trading_scenario, optional: true
  has_one :loop, as: :subject, class_name: "SolidLoop::Loop", dependent: :destroy

  enum :orchestrator_status, {
    initial: "initial",
    running: "running",
    waiting: "waiting",
    stopped: "stopped"
  }, default: :initial, prefix: :orchestrator

  def live?
    return true if agent_batch&.agent_mode == "swiftward"
    return true if agent_id.present?
    false
  end

  def current_virtual_time
    return nil unless loop&.state
    ts = loop.state["virtual_time"]
    ts ? Time.at(ts.to_i) : nil
  end

  def start_at
    agent_batch&.start_at || trading_scenario&.start_at
  end

  def end_at
    agent_batch&.end_at || trading_scenario&.end_at
  end

  def step_interval
    agent_batch&.step_interval || trading_scenario&.step_interval
  end

  def agent
    loop&.agent
  end
end
