class DownloadCandlesJob < ApplicationJob
  include GoodJob::ActiveJobExtensions::Concurrency

  queue_as :default

  good_job_control_concurrency_with(
    key: "DownloadCandlesJob",
    enqueue_limit: 1
  )

  def perform
    start_date = 3.months.ago.beginning_of_day
    end_date = 2.days.ago.end_of_day

    OhlcCandle::TICKERS.each do |ticker|
      BinanceOhlcDataFetcherJob.perform_later(ticker: ticker, start_date: start_date, end_date: end_date)
    end
  end
end
