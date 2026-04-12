class OhlcDataFetcherJob < ApplicationJob
  queue_as :default

  # 5-minute interval is available for up to 2-3 days in Kraken API
  INTERVAL = 5

  def perform(ticker: nil, start_date: nil, end_date: nil)
    tickers = ticker ? [ ticker ] : OhlcCandle::TICKERS
    end_date ||= 2.days.ago.end_of_day
    # Ensure start_date is not too far in the past for 5-min candles
    start_date ||= 3.days.ago.beginning_of_day

    tickers.each do |symbol|
      fetch_for_symbol(symbol, start_date, end_date)
    end
  end

  private

  def fetch_for_symbol(symbol, start_date, end_date)
    # 1. Determine start time
    start_time = if start_date
                   start_date.to_i
                 else
                   last_candle = OhlcCandle.for_symbol(symbol).order(timestamp: :desc).first
                   last_candle ? last_candle.timestamp.to_i + 60 : (end_date - 7.days).to_i
                 end

    Rails.logger.info "[OhlcDataFetcher] Fetching #{symbol} since #{Time.at(start_time)}"

    # 2. Execute kraken-cli
    # Use -o json for parsing and --interval 1 for 1-minute candles
    cmd = "kraken ohlc #{symbol} --interval #{INTERVAL} --since #{start_time} -o json 2>/dev/null"
    output = `#{cmd}`

    if output.blank?
      Rails.logger.warn "[OhlcDataFetcher] No output from kraken-cli for #{symbol}"
      return
    end

    begin
      data = JSON.parse(output)
      # Kraken API returns pair name as key. Let's find the array of candles.
      # It's usually the only key that points to an array (besides "last").
      candles = nil
      data.each do |key, value|
        if value.is_a?(Array) && key != "last"
          candles = value
          break
        end
      end

      unless candles
        Rails.logger.warn "[OhlcDataFetcher] Could not find candle data in JSON for #{symbol}"
        return
      end

      new_records = 0

      candles.each do |c|
        ts = Time.at(c[0].to_i)

        # Apply "not younger than 2 days" rule
        next if ts > end_date

        # Candle structure: [time, open, high, low, close, vwap, volume, count]
        OhlcCandle.find_or_create_by!(symbol: symbol, timestamp: ts) do |candle|
          candle.open = c[1]
          candle.high = c[2]
          candle.low = c[3]
          candle.close = c[4]
          candle.volume = c[6]
          new_records += 1
        end
      end

      Rails.logger.info "[OhlcDataFetcher] Created #{new_records} new candles for #{symbol}"
    rescue JSON::ParserError => e
      Rails.logger.error "[OhlcDataFetcher] Failed to parse JSON for #{symbol}: #{e.message}"
    rescue StandardError => e
      Rails.logger.error "[OhlcDataFetcher] Error processing #{symbol}: #{e.message}"
    end
  end
end
