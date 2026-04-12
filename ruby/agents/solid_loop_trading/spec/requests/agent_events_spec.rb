require 'rails_helper'

RSpec.describe "AgentEvents", type: :request do
  let(:loop_record) { create(:solid_loop_loop) }
  let(:event) { create(:solid_loop_event, loop: loop_record) }

  describe "GET /agent_events" do
    it "returns http success" do
      get agent_events_path
      expect(response).to have_http_status(:success)
    end

    it "filters by loop_id" do
      event
      get agent_events_path, params: { loop_id: loop_record.id }
      expect(response).to have_http_status(:success)
    end

    context "with message_id param" do
      let(:assistant_msg) { create(:solid_loop_message, loop: loop_record, role: "assistant") }
      let(:tool_msg)      { create(:solid_loop_message, loop: loop_record, role: "tool") }

      it "filters by assistant message_id" do
        get agent_events_path, params: { message_id: assistant_msg.id }
        expect(response).to have_http_status(:success)
      end

      it "filters by tool message_id" do
        get agent_events_path, params: { message_id: tool_msg.id }
        expect(response).to have_http_status(:success)
      end
    end
  end

  describe "GET /agent_events/:id" do
    it "returns http success" do
      get agent_event_path(event)
      expect(response).to have_http_status(:success)
    end
  end
end
