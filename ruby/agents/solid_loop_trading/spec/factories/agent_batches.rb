FactoryBot.define do
  factory :agent_batch do
    agent_model { nil }
    concurrency { 1 }
    prefix { "MyString_#{SecureRandom.hex(4)}" }
    attempts { 1 }
    agent_mode { "backtesting" }
    wakeup_interval_minutes { 15 }
  end
end
