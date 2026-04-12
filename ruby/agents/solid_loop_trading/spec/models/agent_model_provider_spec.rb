require 'rails_helper'

RSpec.describe AgentModelProvider, type: :model do
  describe "validations" do
    it "is invalid without a name" do
      provider = AgentModelProvider.new(name: nil)
      expect(provider).not_to be_valid
      expect(provider.errors[:name]).to include("can't be blank")
    end

    it "is invalid without a base_url" do
      provider = AgentModelProvider.new(base_url: nil)
      expect(provider).not_to be_valid
      expect(provider.errors[:base_url]).to include("can't be blank")
    end

    it "is invalid without an api_key" do
      provider = AgentModelProvider.new(api_key: nil)
      expect(provider).not_to be_valid
      expect(provider.errors[:api_key]).to include("can't be blank")
    end
  end

  describe "associations" do
    it "has many agent_models" do
      association = described_class.reflect_on_association(:agent_models)
      expect(association.macro).to eq(:has_many)
      expect(association.options[:dependent]).to eq(:destroy)
    end
  end
end
