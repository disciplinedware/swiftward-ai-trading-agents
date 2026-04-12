FactoryBot.define do
  factory :ohlc_candle do
    symbol { "BTCUSD" }
    timestamp { Time.current }
    open { 50000.0 }
    high { 50500.0 }
    low { 49500.0 }
    close { 50000.0 }
    volume { 1.0 }

    trait :btc do
      symbol { "BTCUSD" }
    end

    trait :eth do
      symbol { "ETHUSD" }
      open { 3000.0 }
      high { 3100.0 }
      low { 2900.0 }
      close { 3000.0 }
    end

    # Трейт для создания "плоской" свечи (удобно для тестов баланса)
    trait :flat do
      params = { open: 50000.0, high: 50000.0, low: 50000.0, close: 50000.0 }
      open { 50000.0 }
      high { 50000.0 }
      low { 50000.0 }
      close { 50000.0 }
    end
    
    # Динамическая цена
    transient do
      price { nil }
    end

    after(:build) do |candle, evaluator|
      if evaluator.price
        candle.open = evaluator.price
        candle.high = evaluator.price
        candle.low = evaluator.price
        candle.close = evaluator.price
      end
    end
  end
end
