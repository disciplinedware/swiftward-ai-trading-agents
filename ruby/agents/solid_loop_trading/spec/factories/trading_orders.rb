FactoryBot.define do
  factory :trading_order do
    trading_session { nil }
    symbol { "MyString" }
    side { "MyString" }
    amount { "9.99" }
    price { "9.99" }
    status { "MyString" }
    equity_at_execution { "9.99" }
    order_id { "MyString" }
  end
end
