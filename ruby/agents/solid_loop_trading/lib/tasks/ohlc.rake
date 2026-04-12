namespace :ohlc do
  desc "Загрузить 5-минутные свечи с Binance за март 2026 года"
  task fetch_march: :environment do
    start_date = Time.parse("2026-03-01 00:00:00 UTC")
    end_date = Time.parse("2026-03-31 23:59:59 UTC")

    puts "Enqueuing BinanceOhlcDataFetcherJob for each ticker, March 2026..."
    OhlcCandle::TICKERS.each do |ticker|
      BinanceOhlcDataFetcherJob.perform_later(ticker: ticker, start_date: start_date, end_date: end_date)
    end
    puts "Enqueued #{OhlcCandle::TICKERS.size} jobs. Check GoodJob dashboard or logs for progress."
  end

  desc "Загрузить свечи за произвольный период: rake ohlc:fetch[BTCUSD,2026-03-01,2026-03-05]"
  task :fetch, [ :ticker, :start_date, :end_date ] => :environment do |t, args|
    ticker = args[:ticker].presence
    start_date = args[:start_date] ? Time.parse(args[:start_date]) : 7.days.ago
    end_date = args[:end_date] ? Time.parse(args[:end_date]) : 2.days.ago.end_of_day

    tickers = ticker ? [ ticker ] : OhlcCandle::TICKERS
    puts "Enqueuing fetch for #{tickers.size} ticker(s) from #{start_date} to #{end_date}..."
    tickers.each do |t|
      BinanceOhlcDataFetcherJob.perform_later(ticker: t, start_date: start_date, end_date: end_date)
    end
    puts "Done. Check GoodJob dashboard or logs for progress."
  end
end
