class OhlcCandle < ApplicationRecord
  validates :symbol, presence: true
  validates :timestamp, presence: true, uniqueness: { scope: :symbol }

  # Ticker list: 12 popular + 13th TON
  TICKERS = %w[BTCUSD ETHUSD SOLUSD ADAUSD DOTUSD XRPUSD DOGEUSD AVAXUSD LINKUSD LTCUSD MATICUSD SHIBUSD TONUSD].freeze

  scope :for_symbol, ->(symbol) { where(symbol: symbol) }
  scope :before_two_days, -> { where("timestamp <= ?", 2.days.ago.end_of_day) }
end
