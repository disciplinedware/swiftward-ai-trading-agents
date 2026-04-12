require 'rails_helper'

RSpec.describe "AgentBatches", type: :request do
  let(:agent_model) { create(:agent_model) }
  let(:trading_scenario) { create(:trading_scenario) }
  let(:agent_batch) do
    AgentBatch.create!(
      agent_model: agent_model,
      concurrency: 1,
      attempts: 1,
      prefix: "test-batch"
    )
  end

  describe "GET /agent_batches" do
    it "returns http success" do
      agent_batch
      get agent_batches_path
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /agent_batches/:id" do
    it "returns http success" do
      get agent_batch_path(agent_batch)
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /agent_batches/new" do
    it "returns http success" do
      get new_agent_batch_path
      expect(response).to have_http_status(:success)
    end
  end

  describe "POST /agent_batches" do
    it "creates batch and agent runs, redirects" do
      post agent_batches_path, params: {
        agent_batch: {
          agent_model_id: agent_model.id,
          concurrency: 1,
          attempts: 1,
          prefix: "new-batch",
          trading_scenario_ids: [ trading_scenario.id ]
        }
      }
      expect(response).to have_http_status(:redirect)
      expect(AgentBatch.count).to eq(1)
    end

    it "renders new when no scenarios selected" do
      post agent_batches_path, params: {
        agent_batch: {
          agent_model_id: agent_model.id,
          concurrency: 1,
          attempts: 1,
          prefix: "new-batch",
          trading_scenario_ids: [ "" ]
        }
      }
      expect(response).to have_http_status(:unprocessable_content)
    end
  end

  describe "POST /agent_batches/:id/start" do
    it "redirects and transitions out of init" do
      post start_agent_batch_path(agent_batch)
      expect(response).to have_http_status(:redirect)
      expect(agent_batch.reload.init?).to be false
    end
  end

  describe "POST /agent_batches/:id/stop" do
    it "stops started batch and redirects" do
      agent_batch.update!(status: :started)
      post stop_agent_batch_path(agent_batch)
      expect(response).to have_http_status(:redirect)
      expect(agent_batch.reload.stopped?).to be true
    end
  end

  describe "POST /agent_batches/:id/restart_errors" do
    it "redirects" do
      agent_batch.update!(status: :error)
      post restart_errors_agent_batch_path(agent_batch)
      expect(response).to have_http_status(:redirect)
    end
  end

  describe "DELETE /agent_batches/:id" do
    it "destroys with correct confirmation" do
      expected = "#{agent_model.name}/#{agent_batch.prefix}"
      expect {
        delete agent_batch_path(agent_batch), params: { confirmation: expected }
      }.to change(AgentBatch, :count).by(-1)
      expect(response).to have_http_status(:redirect)
    end

    it "rejects wrong confirmation" do
      agent_batch # ensure created before the block
      expect {
        delete agent_batch_path(agent_batch), params: { confirmation: "wrong" }
      }.not_to change(AgentBatch, :count)
      expect(response).to have_http_status(:redirect)
    end
  end
end
