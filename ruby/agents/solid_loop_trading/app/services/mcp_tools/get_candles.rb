require "csv"

module McpTools
  class GetCandles < BaseTool
    ALLOWED_INTERVALS = [1, 5, 15, 30, 60, 240, 1440].freeze

    SCHEMA = {
      name: "get_candles",
      description: "Fetch OHLCV candlestick data for a trading pair. 1-minute records are aggregated into the requested interval. Returns a preview and saves the full dataset to CSV for analysis.",
      inputSchema: {
        type: "object",
        properties: {
          pair: { type: "string", description: "Trading pair (e.g. BTCUSD)" },
          interval: { type: "integer", enum: ALLOWED_INTERVALS, description: "Candle interval in minutes" },
          start_time: { type: ["string", "integer"], description: "Start of the range. Prefer ISO 8601 string (e.g. '2025-02-15T13:00:00Z'). Unix timestamp (integer) also accepted." },
          end_time: { type: ["string", "integer"], description: "End of the range. Prefer ISO 8601 string (e.g. '2025-02-15T14:00:00Z'). Unix timestamp (integer) also accepted. Defaults to now." },
          reasoning: { type: "string", description: "Why you are fetching this data — what pattern, signal, or hypothesis you are investigating (e.g. 'checking support levels before placing a buy order', 'looking for trend direction on 1h')" }
        },
        required: ["pair", "interval", "start_time", "reasoning"]
      }
    }.freeze

    def call
      session.sync_state!
      
      pair = arguments["pair"]
      interval = arguments["interval"].to_i
      start_ts = parse_time_arg(arguments["start_time"])
      end_ts = arguments["end_time"] ? parse_time_arg(arguments["end_time"]) : session.virtual_now.to_i

      return "Error: 'pair' is required" if pair.blank?
      unless OhlcCandle::TICKERS.include?(pair)
        return "Error: Unknown pair '#{pair}'. Supported pairs are: #{OhlcCandle::TICKERS.join(', ')}"
      end
      return "Error: Invalid interval. Supported: #{ALLOWED_INTERVALS.join(', ')}" unless ALLOWED_INTERVALS.include?(interval)
      
      v_now = session.virtual_now.to_i
      end_ts = v_now if end_ts > v_now
      
      return "Error: start_time must be before end_time." if start_ts >= end_ts

      raw = OhlcCandle.for_symbol(pair).where(timestamp: Time.at(start_ts)..Time.at(end_ts)).order(timestamp: :asc)
      return "Error: No market data found for #{pair} in range #{Time.at(start_ts).utc} to #{Time.at(end_ts).utc}" if raw.empty?

      interval_sec = interval * 60
      agg = raw.group_by { |c| (c.timestamp.to_i / interval_sec) * interval_sec }.map do |ts, slice|
        {
          timestamp: ts,
          open: slice.first.open.to_f,
          high: slice.map { |c| c.high.to_f }.max,
          low: slice.map { |c| c.low.to_f }.min,
          close: slice.last.close.to_f,
          volume: slice.sum { |c| c.volume.to_f }.round(8)
        }
      end

      # CSV generation
      csv_fn = "#{pair}-#{start_ts}-#{end_ts}.csv"
      # Use workdir_uuid if available (synchronized across MCPs), else fallback to session.uuid
      folder_name = session.workdir_uuid.presence || session.uuid
      csv_dir = Rails.root.join("storage", "external", "data", folder_name)
      FileUtils.mkdir_p(csv_dir)
      
      CSV.open(csv_dir.join(csv_fn), "wb") do |csv|
        csv << ["Timestamp", "Open", "High", "Low", "Close", "Volume"]
        agg.each { |c| csv << [c[:timestamp], c[:open], c[:high], c[:low], c[:close], c[:volume]] }
      end

      headers = "Timestamp | Open | High | Low | Close | Volume"
      format_row = ->(c) { "[#{Time.at(c[:timestamp]).utc}] O:#{c[:open]} H:#{c[:high]} L:#{c[:low]} C:#{c[:close]} V:#{c[:volume]}" }

      summary = ["Success: Market data retrieved.", "Columns: #{headers}"]

      if agg.size <= 32
        agg.each { |c| summary << format_row.call(c) }
      else
        summary << "First 10:"
        agg.first(10).each { |c| summary << format_row.call(c) }
        summary << "..."
        summary << "Sampled (10%..90%):"
        sampled = (1..9).map { |p| agg[(agg.size * p / 10.0).round] }.uniq
        sampled.each { |c| summary << format_row.call(c) }
        summary << "..."
        summary << "Last 10:"
        agg.last(10).each { |c| summary << format_row.call(c) }
      end

      first_row = agg.first
      summary << "\nTotal: #{agg.size} candles. CSV: #{csv_fn}"
      summary << "CSV sample (first 2 lines):"
      summary << "Timestamp,Open,High,Low,Close,Volume"
      summary << "#{first_row[:timestamp]},#{first_row[:open]},#{first_row[:high]},#{first_row[:low]},#{first_row[:close]},#{first_row[:volume]}"
      
      summary.join("\n")
    end

    private

    def parse_time_arg(val)
      return val.to_i if val.is_a?(Integer) || val.to_s.match?(/\A\d+\z/)
      Time.parse(val.to_s).utc.to_i
    end
  end
end
