require 'rails_helper'

RSpec.describe "AgentMessages", type: :request do
  let(:message) { create(:solid_loop_message) }

  describe "GET /show" do
    it "returns http success" do
      execution = create(:agent_run)
      # ensure execution has a loop
      loop_record = execution.loop || execution.create_loop(title: "Test")
      msg = create(:solid_loop_message, loop: loop_record)

      get agent_run_message_path(execution, msg, format: :json)
      expect(response).to have_http_status(:success)
    end
  end
end
