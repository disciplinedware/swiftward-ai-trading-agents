class AgentCostService
  def self.apply_if_missing(message, agent_model:, loop_record:)
    return unless message.role == "assistant" && message.cost.to_f == 0.0
    return if agent_model.price_per_input_token.to_f == 0.0 && agent_model.price_per_output_token.to_f == 0.0

    non_cached = message.tokens_prompt.to_i - message.tokens_prompt_cached.to_i
    cost = (non_cached * agent_model.price_per_input_token.to_f +
            message.tokens_prompt_cached.to_i * agent_model.price_per_cached_token.to_f +
            message.tokens_completion.to_i * agent_model.price_per_output_token.to_f) / 1_000_000.0

    return if cost == 0.0

    message.update_column(:cost, cost)
    SolidLoop::Loop.update_counters(loop_record.id, cost: cost)
  end
end
