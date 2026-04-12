FactoryBot.define do
  factory :agent_model_provider do
    name { "OpenAI" }
    base_url { "https://api.openai.com/v1" }
    api_key { "sk-test-key" }
    description { "Test provider" }
  end
end
