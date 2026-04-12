FactoryBot.define do
  factory :agent_run do
    association :agent_model
    system_prompt { "MyText" }
    user_prompt { "MyText" }
  end
end
