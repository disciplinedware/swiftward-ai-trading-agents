FactoryBot.define do
  factory :ledger_entry do
    trading_session { nil }
    trading_order { nil }
    asset { "MyString" }
    amount { "9.99" }
    category { "MyString" }
  end
end
