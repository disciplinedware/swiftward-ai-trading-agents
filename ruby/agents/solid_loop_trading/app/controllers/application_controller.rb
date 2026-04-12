class ApplicationController < ActionController::Base
  layout "app_shell"
  allow_browser versions: :modern
  stale_when_importmap_changes

  before_action :set_onboarding_step

  private

  def set_onboarding_step
    return if onboarded?

    @onboarding_step = if AgentModelProvider.count.zero?
      { message: "Create a Model Provider to connect an LLM API.", link_text: "New Provider", link_path: new_agent_model_provider_path }
    elsif AgentModel.count.zero?
      { message: "Create an Agent Model to configure your trading LLM.", link_text: "New Model", link_path: new_agent_model_path }
    elsif AgentBatch.count.zero?
      { message: "Create a Batch to run trading backtests.", link_text: "Go to Batches", link_path: agent_batches_path }
    elsif !AgentRun.where(orchestrator_status: [ :running, :waiting ]).exists?
      batch = AgentBatch.where.not(status: :init).order(created_at: :desc).first
      if batch&.warm_up_not_started? || batch&.warm_up_pending?
        { message: "Warm up your model before starting runs.", link_text: "Warm Up", link_path: warm_up_agent_batch_path(batch) }
      else
        { message: "Start agent runs from an existing batch.", link_text: "Go to Batches", link_path: agent_batches_path }
      end
    end
  end

  helper_method :onboarded?

  def onboarded?
    SolidLoop::Loop.where.not(status: :init).any?
  end
end
