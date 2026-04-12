class AddUniqueIndexToOhlcCandlesOnSymbolAndTimestamp < ActiveRecord::Migration[8.1]
  def change
    add_index :ohlc_candles, [ :symbol, :timestamp ], unique: true, name: "idx_ohlc_candles_symbol_timestamp"
  end
end
