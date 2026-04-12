require 'rails_helper'

RSpec.describe "BacktestingData", type: :request do
  describe "GET /backtesting_data/dashboard" do
    it "returns http success" do
      get dashboard_backtesting_data_path
      expect(response).to have_http_status(:success)
    end

    it "renders news stats" do
      get dashboard_backtesting_data_path
      expect(response.body).to include("News")
      expect(response.body).to include("items")
    end

    it "renders candles stats" do
      get dashboard_backtesting_data_path
      expect(response.body).to include("Candles")
    end

    it "renders actions" do
      get dashboard_backtesting_data_path
      expect(response.body).to include("Download Candles")
      expect(response.body).to include("Parse News")
    end
  end

  describe "POST /backtesting_data/download_candles" do
    it "redirects to dashboard" do
      allow(BinanceOhlcDataFetcherJob).to receive(:perform_later)
      post download_candles_backtesting_data_path
      expect(response).to redirect_to(dashboard_backtesting_data_path)
    end
  end

  describe "POST /backtesting_data/parse_news" do
    it "redirects to dashboard" do
      allow(ParseNewsJob).to receive(:perform_later)
      post parse_news_backtesting_data_path
      expect(response).to redirect_to(dashboard_backtesting_data_path)
    end
  end
end

RSpec.describe "AgentRuns index", type: :request do
  describe "GET /" do
    it "returns http success" do
      get agent_runs_path
      expect(response).to have_http_status(:success)
    end
  end
end
