class AgentRunsQuery
  def initialize(relation = AgentRun.all)
    @relation = relation
  end

  def call(params = {})
    # TODO: SolidLoop::Message is polymorphic, so counting messages might require a join through solid_loop_loops
    scoped = @relation.left_joins(loop: :messages)
                       .group("agent_runs.id")
                       .select("agent_runs.*, COUNT(solid_loop_messages.id) AS messages_count")
                       .includes(:agent_model, :trading_scenario, :agent_batch)
                       .order(created_at: :desc)

    scoped = scoped.where(trading_scenario_id: params[:trading_scenario_id]) if params[:trading_scenario_id].present?
    scoped = scoped.where(agent_batch_id: params[:agent_batch_id]) if params[:agent_batch_id].present?
    scoped = scoped.where(agent_model_id: params[:agent_model_id]) if params[:agent_model_id].present?

    if params[:query].present?
      scoped = scoped.where("agent_runs.title ILIKE ?", "%#{params[:query]}%")
    end

    scoped = scoped.page(params[:page]).per(params[:per] || 50)

    Result.new(scoped)
  end

  class Result
    attr_reader :executions
    def initialize(executions)
      @executions = executions
    end
  end
end
