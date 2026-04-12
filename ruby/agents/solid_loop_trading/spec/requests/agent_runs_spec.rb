require 'rails_helper'

RSpec.describe "AgentRuns", type: :request do
  let(:agent_run) { create(:agent_run) }
  let(:mock_agent) { instance_double(TradingAgent) }

  describe "GET /agent_runs" do
    it "returns http success" do
      agent_run
      get agent_runs_path
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /agent_runs/:id" do
    it "returns http success" do
      get agent_run_path(agent_run)
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /agent_runs/new" do
    it "returns http success" do
      get new_agent_run_path
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /agent_runs/:id/edit" do
    it "returns http success" do
      get edit_agent_run_path(agent_run)
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /agent_runs/:id/tools" do
    it "returns http success" do
      get tools_agent_run_path(agent_run)
      expect(response).to have_http_status(:success)
    end
  end

  describe "GET /agent_runs/:id/mcp" do
    it "returns http success without session" do
      get mcp_agent_run_path(agent_run)
      expect(response).to have_http_status(:success)
    end
  end

  describe "POST /agent_runs" do
    it "creates without user_prompt and redirects" do
      post agent_runs_path, params: {
        agent_run: { agent_model_id: create(:agent_model).id, title: "Test Run" }
      }
      expect(response).to have_http_status(:redirect)
    end

    it "creates with user_prompt, calls TradingAgent.start!, redirects" do
      allow(TradingAgent).to receive(:start!)
      post agent_runs_path, params: {
        agent_run: { agent_model_id: create(:agent_model).id, title: "Test" },
        user_prompt: "Analyse market"
      }
      expect(TradingAgent).to have_received(:start!)
      expect(response).to have_http_status(:redirect)
    end

    it "renders new on invalid params" do
      post agent_runs_path, params: { agent_run: { agent_model_id: nil } }
      expect(response).to have_http_status(:unprocessable_content)
    end
  end

  describe "PATCH /agent_runs/:id" do
    it "updates and redirects" do
      patch agent_run_path(agent_run), params: { agent_run: { title: "Updated" } }
      expect(response).to have_http_status(:redirect)
    end

    it "renders edit on invalid params" do
      patch agent_run_path(agent_run), params: { agent_run: { agent_model_id: nil } }
      expect(response).to have_http_status(:unprocessable_content)
    end
  end

  describe "DELETE /agent_runs/:id" do
    it "destroys and redirects" do
      agent_run
      expect {
        delete agent_run_path(agent_run)
      }.to change(AgentRun, :count).by(-1)
      expect(response).to have_http_status(:redirect)
    end
  end

  describe "POST /agent_runs/:id/pause" do
    it "calls agent.pause! and redirects" do
      allow(agent_run).to receive(:agent).and_return(mock_agent)
      allow(AgentRun).to receive(:find).and_return(agent_run)
      expect(mock_agent).to receive(:pause!)

      post pause_agent_run_path(agent_run)
      expect(response).to have_http_status(:redirect)
    end
  end

  describe "POST /agent_runs/:id/resume" do
    it "calls agent.resume! and redirects with notice on success" do
      allow(agent_run).to receive(:agent).and_return(mock_agent)
      allow(AgentRun).to receive(:find).and_return(agent_run)
      allow(mock_agent).to receive(:resume!).and_return(true)

      post resume_agent_run_path(agent_run)
      expect(response).to redirect_to(agent_run_path(agent_run))
      expect(flash[:notice]).to be_present
    end

    it "redirects with alert when resume fails" do
      allow(agent_run).to receive(:agent).and_return(mock_agent)
      allow(AgentRun).to receive(:find).and_return(agent_run)
      allow(mock_agent).to receive(:resume!).and_return(nil)

      post resume_agent_run_path(agent_run)
      expect(response).to redirect_to(agent_run_path(agent_run))
      expect(flash[:alert]).to be_present
    end
  end

  describe "GET /agent_runs/:id/download" do
    it "sends html file" do
      SolidLoop::Loop.create!(subject: agent_run, title: "Test", status: :paused)
      get download_agent_run_path(agent_run)
      expect(response).to have_http_status(:success)
      expect(response.headers["Content-Disposition"]).to include("attachment")
    end
  end

  describe "POST /agent_runs/recalculate_all" do
    it "redirects with notice" do
      post recalculate_all_agent_runs_path
      expect(response).to have_http_status(:redirect)
      expect(flash[:notice]).to be_present
    end
  end

  describe "POST /agent_runs/:id/run_mcp_tool" do
    it "renders turbo_stream with error when no session" do
      post run_mcp_tool_agent_run_path(agent_run), params: { tool_name: "test_tool" },
           headers: { "Accept" => "text/vnd.turbo-stream.html" }
      expect(response).to have_http_status(:success)
    end
  end
end
