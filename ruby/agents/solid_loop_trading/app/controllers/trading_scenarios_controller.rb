class TradingScenariosController < ApplicationController
  before_action :set_trading_scenario, only: %i[ show edit update destroy ]

  def index
    @trading_scenarios = TradingScenario.left_joins(:agent_runs)
                            .group(:id)
                            .select("trading_scenarios.*, COUNT(agent_runs.id) AS conversations_count")
                            .order("filename ASC NULLS LAST, name ASC")
  end

  def show
    result = AgentRunsQuery.new(@trading_scenario.agent_runs).call(params)
    @agent_runs = result.executions
  end

  def new
    @trading_scenario = TradingScenario.new
  end

  def edit
  end

  def create
    @trading_scenario = TradingScenario.new(trading_scenario_params)

    if @trading_scenario.save
      redirect_to trading_scenarios_path, notice: "Trading scenario was successfully created."
    else
      render :new, status: :unprocessable_content
    end
  end

  def update
    if @trading_scenario.update(trading_scenario_params)
      redirect_to trading_scenarios_path, notice: "Trading scenario was successfully updated."
    else
      render :edit, status: :unprocessable_content
    end
  end

  def destroy
    @trading_scenario.destroy
    redirect_to trading_scenarios_path, notice: "Trading scenario was successfully destroyed."
  end

  private
    def set_trading_scenario
      @trading_scenario = TradingScenario.find(params[:id])
    end

    def trading_scenario_params
      params.require(:trading_scenario).permit(:name, :prompt, :filename, :user_prompt, :start_at, :end_at, :step_interval)
    end
end
