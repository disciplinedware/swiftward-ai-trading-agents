json.extract! agent_model, :id, :name, :llm_model_name, :base_url, :api_token, :description, :created_at, :updated_at
json.url agent_model_url(agent_model, format: :json)
