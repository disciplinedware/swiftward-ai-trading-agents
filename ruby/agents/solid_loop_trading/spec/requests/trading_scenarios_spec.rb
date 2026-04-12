require 'rails_helper'

RSpec.describe "TradingScenarios", type: :request do
  let(:trading_scenario) { create(:trading_scenario) }

  describe "GET /trading_scenarios" do
    it "returns http success" do
      trading_scenario
      get trading_scenarios_path
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /trading_scenarios/:id" do
    it "returns http success" do
      get trading_scenario_path(trading_scenario)
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /trading_scenarios/new" do
    it "returns http success" do
      get new_trading_scenario_path
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /trading_scenarios/:id/edit" do
    it "returns http success" do
      get edit_trading_scenario_path(trading_scenario)
      expect(response).to have_http_status(:success)
    end
  end

  describe "POST /trading_scenarios" do
    it "creates and redirects" do
      post trading_scenarios_path, params: {
        trading_scenario: { name: "New Scenario", prompt: "Trade BTC" }
      }
      expect(response).to have_http_status(:redirect)
    end
  end

  describe "PATCH /trading_scenarios/:id" do
    it "updates and redirects" do
      patch trading_scenario_path(trading_scenario), params: {
        trading_scenario: { name: "Updated Scenario" }
      }
      expect(response).to have_http_status(:redirect)
    end
  end

  describe "DELETE /trading_scenarios/:id" do
    it "destroys and redirects" do
      trading_scenario
      expect {
        delete trading_scenario_path(trading_scenario)
      }.to change(TradingScenario, :count).by(-1)
      expect(response).to have_http_status(:redirect)
    end
  end
end
