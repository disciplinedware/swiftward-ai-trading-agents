require 'rails_helper'

RSpec.describe "TradingSessions", type: :request do
  let!(:trading_session) { TradingSession.create!(uuid: SecureRandom.uuid) }

  describe "GET /index" do
    it "returns http success" do
      get trading_sessions_path
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /show" do
    it "returns http success" do
      get trading_session_path(trading_session.uuid)
      expect(response).to have_http_status(:success)
    end
  end
end
