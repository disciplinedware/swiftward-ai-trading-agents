class CreateOhlcCandles < ActiveRecord::Migration[8.1]
  def change
    create_table :ohlc_candles do |t|
      t.string :symbol
      t.datetime :timestamp
      t.decimal :open
      t.decimal :high
      t.decimal :low
      t.decimal :close
      t.decimal :volume

      t.timestamps
    end

    add_index :ohlc_candles, :symbol
    add_index :ohlc_candles, :timestamp
  end
end
