FactoryBot.define do
  factory :agent_model do
    association :provider, factory: :agent_model_provider
    name { "Test Llama" }
    llm_model_name { "llama-3-test" }
    tools { [] }

    trait :deepseek do
      name { "DeepSeek-V3" }
      llm_model_name { "deepseek-chat" }
      tools { ["McpGeneratedReadFile", "McpGeneratedWriteFile", "McpGeneratedListFiles"] }
    end
  end

  factory :solid_loop_loop, class: "SolidLoop::Loop" do
    title { "Test Conversation" }
    status { :queued }
  end

  # Legacy alias
  factory :agent_conversation, parent: :solid_loop_loop

  factory :solid_loop_message, class: "SolidLoop::Message" do
    association :loop, factory: :solid_loop_loop
    role { "user" }
    content { "Hello" }
    status { "pending" }

    trait :system do
      role { "system" }
      content { "You are a helpful assistant." }
    end

    trait :assistant do
      role { "assistant" }
      content { "I am an assistant." }
    end

    trait :tool do
      role { "tool" }
      content { "Tool result." }
    end
  end

  # Legacy alias
  factory :agent_message, parent: :solid_loop_message

  factory :solid_loop_tool_call, class: "SolidLoop::ToolCall" do
    association :message, factory: :solid_loop_message
    tool_call_id { "call_#{SecureRandom.hex(4)}" }
    function_name { "test_tool" }
    arguments { {} }
  end

  # Legacy alias
  factory :agent_tool_call, parent: :solid_loop_tool_call

  factory :solid_loop_event, class: "SolidLoop::Event" do
    name { "llm_completion" }
    http_method { "POST" }
    association :loop, factory: :solid_loop_loop
    eventable { association :solid_loop_message }
  end

  # Legacy alias
  factory :agent_event, parent: :solid_loop_event
end
