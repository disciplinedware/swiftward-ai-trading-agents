class AgentModelProvidersController < ApplicationController
  before_action :set_agent_model_provider, only: %i[ show edit update destroy ]

  def index
    @agent_model_providers = AgentModelProvider.all.order(name: :asc)
  end

  def show
  end

  def new
    @agent_model_provider = AgentModelProvider.new
  end

  def edit
  end

  def create
    @agent_model_provider = AgentModelProvider.new(agent_model_provider_params)

    if @agent_model_provider.save
      redirect_to @agent_model_provider, notice: "Agent model provider was successfully created."
    else
      render :new, status: :unprocessable_content
    end
  end

  def update
    if @agent_model_provider.update(agent_model_provider_params)
      redirect_to @agent_model_provider, notice: "Agent model provider was successfully updated."
    else
      render :edit, status: :unprocessable_content
    end
  end

  def destroy
    @agent_model_provider.destroy!
    redirect_to agent_model_providers_path, notice: "Agent model provider was successfully destroyed."
  end

  private

  def set_agent_model_provider
    @agent_model_provider = AgentModelProvider.find(params[:id])
  end

  def agent_model_provider_params
    params.require(:agent_model_provider).permit(:name, :base_url, :api_key, :description)
  end
end
