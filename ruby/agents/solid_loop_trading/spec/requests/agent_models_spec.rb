require 'rails_helper'

RSpec.describe "AgentModels", type: :request do
  let(:provider) { create(:agent_model_provider) }
  let(:agent_model) { create(:agent_model, provider: provider) }

  describe "GET /agent_models" do
    it "returns http success" do
      agent_model
      get agent_models_path
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /agent_models/:id" do
    it "returns http success" do
      get agent_model_path(agent_model)
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /agent_models/new" do
    it "returns http success" do
      get new_agent_model_path
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /agent_models/:id/edit" do
    it "returns http success" do
      get edit_agent_model_path(agent_model)
      expect(response).to have_http_status(:success)
    end
  end

  describe "POST /agent_models" do
    it "creates and redirects" do
      post agent_models_path, params: {
        agent_model: {
          name: "Test Model",
          llm_model_name: "gpt-4",
          agent_model_provider_id: provider.id,
          tools: []
        }
      }
      expect(response).to have_http_status(:redirect)
    end
  end

  describe "PATCH /agent_models/:id" do
    it "updates and redirects" do
      patch agent_model_path(agent_model), params: {
        agent_model: { name: "Updated Name" }
      }
      expect(response).to have_http_status(:redirect)
    end
  end

  describe "DELETE /agent_models/:id" do
    it "destroys and redirects" do
      agent_model
      expect {
        delete agent_model_path(agent_model)
      }.to change(AgentModel, :count).by(-1)
      expect(response).to have_http_status(:redirect)
    end
  end
end
