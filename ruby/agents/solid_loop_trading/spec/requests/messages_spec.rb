require 'rails_helper'

RSpec.describe "Messages", type: :request do
  let(:agent_run) { create(:agent_run) }
  let(:loop) do
    SolidLoop::Loop.create!(subject: agent_run, title: "Test", status: :paused)
  end
  let(:message) do
    loop.messages.create!(role: "user", content: "Hello", status: "pending")
  end

  describe "GET /agent_runs/:agent_run_id/messages/:id" do
    it "returns http success" do
      get agent_run_message_path(agent_run, message, format: :json)
      expect(response).not_to have_http_status(:internal_server_error)
    end
  end

  describe "GET /agent_runs/:agent_run_id/messages/:id/edit" do
    it "returns http success" do
      get edit_agent_run_message_path(agent_run, message, format: :json)
      expect(response).not_to have_http_status(:internal_server_error)
    end
  end
end
