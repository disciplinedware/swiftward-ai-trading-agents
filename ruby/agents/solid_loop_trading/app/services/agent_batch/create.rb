class AgentBatch::Create
  AGENT_CLASS_MAP = {
    "backtesting" => TradingAgent,
    "swiftward"   => SwiftwardTradingAgent
  }.freeze

  def self.call(agent_batch:, trading_scenario_ids:)
    trading_scenarios = TradingScenario.where(id: trading_scenario_ids)

    return false if trading_scenarios.empty?

    agent_class = AGENT_CLASS_MAP.fetch(agent_batch.agent_mode, TradingAgent)

    agent_batch.transaction do
      if agent_batch.save
        trading_scenarios.each do |task|
          agent_batch.attempts.times do |attempt|
            # Simulation time parameters: prioritize benchmark values, then task values
            final_start_at = agent_batch.start_at || task.start_at
            final_end_at = agent_batch.end_at || task.end_at
            final_step_interval = agent_batch.step_interval || task.step_interval

            system_content = task.prompt.to_s
            # system_content += "\n\nFile: #{task.filename}" if task.filename.present?

            user_content = task.user_prompt.present? ? task.user_prompt : (task.filename.present? ? "Parse file #{task.filename}" : "Please start the task")

            execution = AgentRun.create!(
              agent_model: agent_batch.agent_model,
              trading_scenario: task,
              agent_batch: agent_batch,
              system_prompt: system_content,
              user_prompt: user_content,
              filename: task.filename,
              title: agent_batch.prefix.present? ? "[#{agent_batch.prefix}] #{task.name} ##{attempt + 1}" : "#{task.name} ##{attempt + 1}",
              wakeup_interval_minutes: agent_batch.wakeup_interval_minutes
            )

            loop_state = {}
            # Swiftward is live trading — no simulation clock needed
            loop_state[:virtual_time] = final_start_at.to_i if final_start_at && agent_batch.agent_mode == "backtesting"

            agent_class.create!(
              subject: execution,
              message: user_content,
              filename: task.filename,
              **loop_state
            )
          end
        end
        true
      else
        false
      end
    end
  end
end
