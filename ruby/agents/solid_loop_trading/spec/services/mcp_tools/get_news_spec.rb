require "rails_helper"

RSpec.describe McpTools::GetNews do
  let(:session) { TradingSession.create!(uuid: SecureRandom.uuid) }
  let(:base_time) { Time.parse("2026-03-01 12:00:00 UTC") }

  before do
    session.set_virtual_time(base_time)
    
    # News in the past
    NewsItem.create!(title: "Past News", content: "Something happened", published_at: base_time - 1.hour)
    # News exactly now
    NewsItem.create!(title: "Current News", content: "Happening now", published_at: base_time)
    # News in the future
    NewsItem.create!(title: "Future News", content: "Will happen later", published_at: base_time + 1.hour)
  end

  describe "#call" do
    it "returns only past and current news" do
      result = described_class.new(session, {}).call
      
      expect(result).to include("Past News")
      expect(result).to include("Current News")
      expect(result).not_to include("Future News")
    end

    it "filters by keywords" do
      NewsItem.create!(title: "Bitcoin ETF Approved", published_at: base_time - 10.minutes)
      
      result = described_class.new(session, { "keywords" => "ETF" }).call
      expect(result).to include("Bitcoin ETF Approved")
      expect(result).not_to include("Past News")
    end

    it "respects virtual now after time move" do
      # Move time forward by 2 hours
      session.set_virtual_time(base_time + 2.hours)
      
      result = described_class.new(session, {}).call
      expect(result).to include("Future News") # Now it should be visible
    end
  end
end
