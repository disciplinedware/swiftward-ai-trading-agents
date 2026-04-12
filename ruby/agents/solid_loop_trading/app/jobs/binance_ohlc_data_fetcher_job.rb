class BinanceOhlcDataFetcherJob < ApplicationJob
  queue_as :default

  BASE_URL = "https://api.binance.com/api/v3/klines"

  SYMBOL_MAPPING = {
    "BTCUSD" => "BTCUSDT",
    "ETHUSD" => "ETHUSDT",
    "SOLUSD" => "SOLUSDT",
    "ADAUSD" => "ADAUSDT",
    "DOTUSD" => "DOTUSDT",
    "XRPUSD" => "XRPUSDT",
    "DOGEUSD" => "DOGEUSDT",
    "AVAXUSD" => "AVAXUSDT",
    "LINKUSD" => "LINKUSDT",
    "LTCUSD" => "LTCUSDT",
    "MATICUSD" => "MATICUSDT",
    "SHIBUSD" => "SHIBUSDT",
    "TONUSD" => "TONUSDT"
  }.freeze

  def perform(ticker:, start_date: nil, end_date: nil)
    end_date ||= 2.days.ago.end_of_day
    start_date ||= 3.days.ago.beginning_of_day

    fetch_for_symbol(ticker, start_date, end_date)
  end

  private

  def fetch_for_symbol(symbol, start_date, end_date)
    binance_symbol = SYMBOL_MAPPING[symbol] || "#{symbol}T"

    current_start = start_date.to_i * 1000
    final_end = end_date.to_i * 1000

    Rails.logger.info "[BinanceFetcher] Fetching #{symbol} (#{binance_symbol}) from #{start_date} to #{end_date}"

    total_upserted = 0

    while current_start < final_end
      uri = URI("#{BASE_URL}?symbol=#{binance_symbol}&interval=5m&startTime=#{current_start}&limit=1000")

      response = Net::HTTP.get_response(uri)

      unless response.is_a?(Net::HTTPSuccess)
        Rails.logger.error "[BinanceFetcher] API Error for #{symbol}: #{response.code} #{response.body}"
        break
      end

      klines = JSON.parse(response.body)
      break if klines.empty?

      records = []
      last_ts = current_start

      klines.each do |k|
        ts_ms = k[0].to_i
        break if ts_ms > final_end

        ts = Time.at(ts_ms / 1000)
        records << {
          symbol: symbol,
          timestamp: ts,
          open: k[1],
          high: k[2],
          low: k[3],
          close: k[4],
          volume: k[5],
          created_at: Time.current,
          updated_at: Time.current
        }
        last_ts = ts_ms
      end

      if records.any?
        OhlcCandle.upsert_all(records, unique_by: [ :symbol, :timestamp ])
        total_upserted += records.size
      end

      current_start = last_ts + 300_000
      sleep(0.15)
    end

    Rails.logger.info "[BinanceFetcher] Done #{symbol}: #{total_upserted} candles upserted"
  rescue => e
    Rails.logger.error "[BinanceFetcher] Fatal error for #{symbol}: #{e.class} #{e.message}"
  end
end
