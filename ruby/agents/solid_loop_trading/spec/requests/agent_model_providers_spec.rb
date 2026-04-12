require 'rails_helper'

RSpec.describe "AgentModelProviders", type: :request do
  let(:valid_attributes) {
    {
      name: "OpenAI",
      base_url: "https://api.openai.com/v1",
      api_key: "sk-test-key"
    }
  }

  let(:invalid_attributes) {
    {
      name: "",
      base_url: "",
      api_key: ""
    }
  }

  describe "GET /index" do
    it "returns http success" do
      create(:agent_model_provider)
      get agent_model_providers_path
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /show" do
    it "returns http success" do
      provider = create(:agent_model_provider)
      get agent_model_provider_path(provider)
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /new" do
    it "returns http success" do
      get new_agent_model_provider_path
      expect(response).to have_http_status(:success)
    end
  end

  describe "POST /create" do
    context "with valid parameters" do
      it "creates a new AgentModelProvider" do
        expect {
          post agent_model_providers_path, params: { agent_model_provider: valid_attributes }
        }.to change(AgentModelProvider, :count).by(1)
      end

      it "redirects to the created provider" do
        post agent_model_providers_path, params: { agent_model_provider: valid_attributes }
        expect(response).to redirect_to(AgentModelProvider.last)
      end
    end

    context "with invalid parameters" do
      it "does not create a new AgentModelProvider" do
        expect {
          post agent_model_providers_path, params: { agent_model_provider: invalid_attributes }
        }.to change(AgentModelProvider, :count).by(0)
      end

      it "returns an unprocessable entity status" do
        post agent_model_providers_path, params: { agent_model_provider: invalid_attributes }
        expect(response).to have_http_status(:unprocessable_content)
      end
    end
  end

  describe "GET /edit" do
    it "returns http success" do
      provider = create(:agent_model_provider)
      get edit_agent_model_provider_path(provider)
      expect(response).to have_http_status(:success)
    end
  end

  describe "PATCH /update" do
    let(:new_attributes) { { name: "New Name" } }

    context "with valid parameters" do
      it "updates the requested provider" do
        provider = create(:agent_model_provider)
        patch agent_model_provider_path(provider), params: { agent_model_provider: new_attributes }
        provider.reload
        expect(provider.name).to eq("New Name")
      end

      it "redirects to the provider" do
        provider = create(:agent_model_provider)
        patch agent_model_provider_path(provider), params: { agent_model_provider: new_attributes }
        expect(response).to redirect_to(provider)
      end
    end
  end

  describe "DELETE /destroy" do
    it "destroys the requested provider" do
      provider = create(:agent_model_provider)
      expect {
        delete agent_model_provider_path(provider)
      }.to change(AgentModelProvider, :count).by(-1)
    end

    it "redirects to the providers list" do
      provider = create(:agent_model_provider)
      delete agent_model_provider_path(provider)
      expect(response).to redirect_to(agent_model_providers_path)
    end
  end
end
