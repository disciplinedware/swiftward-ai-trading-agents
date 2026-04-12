require 'rails_helper'

RSpec.describe AgentModel, type: :model do
  describe "associations" do
    it "belongs to a provider" do
      association = described_class.reflect_on_association(:provider)
      expect(association.macro).to eq(:belongs_to)
      expect(association.options[:class_name]).to eq('AgentModelProvider')
    end
  end

  describe "validations" do
    it "is invalid without llm_model_name" do
      model = AgentModel.new(llm_model_name: nil)
      expect(model).not_to be_valid
      expect(model.errors[:llm_model_name]).to include("can't be blank")
    end
  end

  describe "name auto-generation" do
    let(:provider) { create(:agent_model_provider, name: "OpenAI") }

    it "sets a default name if blank" do
      model = AgentModel.create!(provider: provider, llm_model_name: "gpt-4")
      expect(model.name).to eq("OpenAI: gpt-4")
    end

    it "does not override name if provided" do
      model = AgentModel.create!(provider: provider, llm_model_name: "gpt-4", name: "Custom Name")
      expect(model.name).to eq("Custom Name")
    end
  end
end
