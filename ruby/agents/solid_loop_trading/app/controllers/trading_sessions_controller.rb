class TradingSessionsController < ApplicationController
  UUID_REGEX = /\A[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\z/i

  def index
    @trading_sessions = TradingSession.all.order(created_at: :desc)
    @remote_loops = SolidLoop::Loop
                      .where(subject_type: "AgentRun", subject_id: AgentRun.where.not(agent_id: nil).select(:id))
                      .includes(:subject)
                      .order(created_at: :desc)
                      .page(params[:remote_page])
                      .per(20)
  end

  def show
    @presenter = build_presenter(params[:id])
  end

  private

  def build_presenter(id)
    local = TradingSession.find_by(uuid: id)
    return LocalTradingSessionPresenter.new(local) if local

    raise ActiveRecord::RecordNotFound unless id.match?(UUID_REGEX)

    agent_run = AgentRun.find_by(agent_id: id)
    raise ActiveRecord::RecordNotFound unless agent_run

    RemoteTradingSessionPresenter.new(agent_run.loop.id)
  end
end
