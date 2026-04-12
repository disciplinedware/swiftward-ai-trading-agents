require "rails_helper"

RSpec.describe AgentModelsController, type: :routing do
  describe "routing" do
    it "routes to #index" do
      expect(get: "/agent_models").to route_to("agent_models#index")
    end

    it "routes to #new" do
      expect(get: "/agent_models/new").to route_to("agent_models#new")
    end

    it "routes to #show" do
      expect(get: "/agent_models/1").to route_to("agent_models#show", id: "1")
    end

    it "routes to #edit" do
      expect(get: "/agent_models/1/edit").to route_to("agent_models#edit", id: "1")
    end


    it "routes to #create" do
      expect(post: "/agent_models").to route_to("agent_models#create")
    end

    it "routes to #update via PUT" do
      expect(put: "/agent_models/1").to route_to("agent_models#update", id: "1")
    end

    it "routes to #update via PATCH" do
      expect(patch: "/agent_models/1").to route_to("agent_models#update", id: "1")
    end

    it "routes to #destroy" do
      expect(delete: "/agent_models/1").to route_to("agent_models#destroy", id: "1")
    end
  end
end
